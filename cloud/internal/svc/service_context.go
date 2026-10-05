package svc

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"onecloud/ent"

	entsql "entgo.io/ent/dialect/sql"
	"onecloud/internal/config"
	"onecloud/internal/pkg/cache"
	"onecloud/internal/pkg/database"
	"onecloud/internal/pkg/jwt"
	"onecloud/internal/pkg/snowId"
	"onecloud/internal/pkg/storage"
	"onecloud/internal/pkg/verification"
	"time"

	"github.com/redis/go-redis/v9"
)

// ServiceContext 服务上下文，集中管理所有依赖
type ServiceContext struct {
	Config    config.Config
	Db        *sql.DB
	EntClient *ent.Client
	Redis     *redis.Client
	Snowflake *snowId.Snowflake     // 雪花id生成器
	Storage   storage.Driver        // 存储驱动
	Verifier  *verification.Service // 验证码服务
}

// NewServiceContext 创建服务上下文，初始化所有依赖
func NewServiceContext(c config.Config) *ServiceContext {
	ctx := &ServiceContext{Config: c}

	// 生产模式禁止使用演示默认密钥或过短密钥（空值/演示值/长度不足均拒绝启动）
	// HS256 安全基线：随机 Secret ≥ 32 字节
	if c.Mode == "prod" {
		if c.JWT.Secret == "" || c.JWT.Secret == "www.topgoer.com" || c.JWT.Secret == "change-me-in-production" {
			panic(fmt.Errorf("JWT Secret 未配置或仍为演示默认值，拒绝在生产模式启动"))
		}
		if len(c.JWT.Secret) < 32 {
			panic(fmt.Errorf("JWT Secret 长度不足（%d < 32），拒绝在生产模式启动", len(c.JWT.Secret)))
		}
	}

	// 初始化 JWT 配置（密钥、过期时间、签发者；空 Secret 在 Init 内直接 panic）
	jwt.Init(c.JWT.Secret, c.JWT.Expire, c.JWT.Issuer)

	// 连接 MySQL
	if err := ctx.initDatabase(); err != nil {
		panic(fmt.Errorf("数据库初始化失败: %v", err))
	}

	// 初始化雪花 ID 生成器
	if err := ctx.initSnowFlake(); err != nil {
		panic(fmt.Errorf("雪花 ID 生成器初始化失败: %v", err))
	}

	// 初始化存储驱动
	if err := ctx.initStorage(); err != nil {
		panic(fmt.Errorf("存储驱动初始化失败: %v", err))
	}

	// 连接 Redis（可选，失败仅告警不中断）
	if err := ctx.initRedis(); err != nil {
		fmt.Printf("Redis 连接失败（可选组件）: %v\n", err)
	} else {
		fmt.Println("Redis 连接成功!")
	}

	// 初始化验证码服务（Redis 未配置时由组件降级为明确报错，不再空指针）
	// LogSender 按 mode 脱码：dev 输出完整验证码，其余模式仅输出 ******
	ctx.Verifier = verification.NewService(cache.NewRedisKV(ctx.Redis), verification.NewLogSender(c.Mode))

	return ctx
}

// initDatabase 初始化数据库连接与 ent 客户端
func (s *ServiceContext) initDatabase() error {
	// 1. 从 dbConfig 拼接 MySQL DSN（ent.Open 需要的格式）
	dbConfig := database.DBConfig{
		Host:            s.Config.MySQL.Host,
		User:            s.Config.MySQL.User,
		Password:        s.Config.MySQL.Password,
		Database:        s.Config.MySQL.Database,
		Charset:         s.Config.MySQL.Charset,
		MaxOpenConns:    s.Config.MySQL.MaxOpenConns,
		MaxIdleConns:    s.Config.MySQL.MaxIdleConns,
		ConnMaxLifetime: s.Config.MySQL.ConnMaxLifetime,
		ConnMaxIdleTime: s.Config.MySQL.ConnMaxIdleTime,
		Debug:           s.Config.MySQL.Debug,
		Loc:             s.Config.MySQL.Loc,
	}

	// 拼接标准 MySQL DSN 字符串
	dsn := fmt.Sprintf("%s:%s@tcp(%s)/%s?charset=%s&parseTime=True&loc=%s",
		dbConfig.User,
		dbConfig.Password,
		dbConfig.Host,
		dbConfig.Database,
		dbConfig.Charset,
		dbConfig.Loc,
	)

	// 2. 创建原始 *sql.DB 连接（便于配置连接池）
	dbConn, err := sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("创建原始数据库连接失败: %w", err)
	}

	// 3. 配置连接池参数
	dbConn.SetMaxOpenConns(dbConfig.MaxOpenConns)
	dbConn.SetMaxIdleConns(dbConfig.MaxIdleConns)
	dbConn.SetConnMaxLifetime(time.Duration(dbConfig.ConnMaxLifetime) * time.Second)
	dbConn.SetConnMaxIdleTime(time.Duration(dbConfig.ConnMaxIdleTime) * time.Second)

	// 4. 验证连接有效性
	if err := dbConn.Ping(); err != nil {
		return fmt.Errorf("数据库连接校验失败: %w", err)
	}

	// 5. 基于同一个 *sql.DB 构建 ent 客户端（复用连接池配置；
	// 原 ent.Open 同 DSN 再开一个池，其一白占连接配置）
	entClient := ent.NewClient(ent.Driver(entsql.OpenDB("mysql", dbConn)))

	// 6. 开发模式下自动迁移
	if s.Config.Mode == "dev" {
		err := entClient.Schema.Create(context.Background())
		if err != nil {
			return fmt.Errorf("数据库自动迁移失败: %w", err)
		}
		fmt.Println("数据库自动迁移成功")
	}

	// 7. 赋值给 ServiceContext
	s.Db = dbConn
	s.EntClient = entClient
	return nil
}

// initRedis 初始化 Redis 连接
func (s *ServiceContext) initRedis() error {
	// 如果没有配置 Redis 则跳过
	if s.Config.RedisConf.Host == "" {
		return errors.New("Redis 未配置")
	}

	redisConfig := cache.RedisConfig{
		Addr:     s.Config.RedisConf.Host,
		Password: s.Config.RedisConf.Password,
		DB:       s.Config.RedisConf.DB,
		PoolSize: s.Config.RedisConf.PoolSize,
	}

	client, err := cache.ConnectRedis(redisConfig)
	if err != nil {
		return err
	}

	s.Redis = client
	return nil
}

// initStorage 初始化存储驱动
func (s *ServiceContext) initStorage() error {
	driver, err := storage.NewDriver(s.Config.Storage)
	if err != nil {
		return err
	}
	s.Storage = driver
	fmt.Printf("存储驱动初始化成功: %s\n", s.Config.Storage.Driver)
	return nil
}

// initSnowFlake 初始化雪花 ID 生成器
func (s *ServiceContext) initSnowFlake() error {
	if snowId.Instance != nil {
		s.Snowflake = snowId.Instance
		return nil
	}
	return errors.New("雪花 ID 生成器单例未初始化")
}

// Close 关闭所有连接
func (s *ServiceContext) Close() {
	if s.EntClient != nil {
		// ent 客户端持有并托管唯一的 *sql.DB（含 s.Db 指向的同一连接池）
		s.EntClient.Close()
	}
	if s.Redis != nil {
		s.Redis.Close()
	}
}
