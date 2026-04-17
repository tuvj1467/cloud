package svc

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"onecloud/ent/ent"
	"onecloud/internal/config"
	"onecloud/internal/pkg/cache"
	"onecloud/internal/pkg/database"
	"onecloud/internal/pkg/snowId"
	"time"

	"github.com/redis/go-redis/v9"
)

type ServiceContext struct {
	Config    config.Config
	Db        *sql.DB
	EntClient *ent.Client
	Redis     *redis.Client
	Snowflake *snowId.Snowflake //雪花id生成器
}

func NewServiceContext(c config.Config) *ServiceContext {
	// 初始化数据库连接
	ctx := &ServiceContext{Config: c}

	// 连接 MySQL
	if err := ctx.initDatabase(); err != nil {
		panic(fmt.Errorf("数据库初始化失败: %v", err))
	}

	if err := ctx.IntSnowFlake(); err != nil {
		fmt.Printf("SnowFlake 加入失败")
	}
	// 连接 Redis（可选）
	//if err := ctx.initRedis(); err != nil {
	//	fmt.Printf("Redis 连接失败（可选组件）: %v\n", err)
	//}

	return ctx
}

func (s *ServiceContext) initDatabase() error {
	// 1. 从dbConfig拼接MySQL DSN（ent.Open需要的格式）
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

	// 拼接标准MySQL DSN字符串（ent.Open的核心参数）
	dsn := fmt.Sprintf("%s:%s@tcp(%s)/%s?charset=%s&parseTime=True&loc=%s",
		dbConfig.User,
		dbConfig.Password,
		dbConfig.Host,
		dbConfig.Database,
		dbConfig.Charset,
		dbConfig.Loc,
	)

	// 2. 先用sql.Open创建原始*sql.DB连接（便于配置连接池）
	dbConn, err := sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("创建原始数据库连接失败: %w", err)
	}

	// 3. 配置连接池参数（复用你的dbConfig配置）
	dbConn.SetMaxOpenConns(dbConfig.MaxOpenConns)
	dbConn.SetMaxIdleConns(dbConfig.MaxIdleConns)
	dbConn.SetConnMaxLifetime(time.Duration(dbConfig.ConnMaxLifetime) * time.Second)
	dbConn.SetConnMaxIdleTime(time.Duration(dbConfig.ConnMaxIdleTime) * time.Second)

	// 4. 验证连接有效性
	if err := dbConn.Ping(); err != nil {
		return fmt.Errorf("数据库连接校验失败: %w", err)
	}

	// 5. 用ent.Open创建ent客户端（两种方式可选）
	// 方式1：直接用DSN创建（简单，无需手动管理*sql.DB）
	entClient, err := ent.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("初始化ent客户端失败: %w", err)
	}
	//defer entClient.Close()

	// 方式2：用已配置的*sql.DB创建（推荐，可自定义连接池参数）
	//entClient, err := ent.NewClient(ent.Driver(sql.OpenDB(dbConn.Driver())))
	//if err != nil {
	//	return fmt.Errorf("初始化ent客户端失败: %w", err)
	//}

	// 6. 开发模式下自动迁移（保留原有逻辑）
	if s.Config.Mode == "dev" {
		err := entClient.Schema.Create(context.Background())

		if err != nil {
			return fmt.Errorf("数据库自动迁移失败: %w", err)
		}
	}
	fmt.Println("数据库自动迁移成功")

	// 7. 赋值给ServiceContext
	s.Db = dbConn // 可选：保留原始*sql.DB连接
	s.EntClient = entClient
	return nil
}

func (s *ServiceContext) initRedis() error {
	// 如果没有配置 Redis 则跳过
	if s.Config.Redis.Addr == "" {
		return nil
	}

	redisConfig := cache.RedisConfig{
		Addr:     s.Config.Redis.Addr,
		Password: s.Config.Redis.Password,
		DB:       s.Config.Redis.DB,
		PoolSize: s.Config.Redis.PoolSize,
	}

	client, err := cache.ConnectRedis(redisConfig)
	if err != nil {
		return err
	}

	s.Redis = client
	return nil
}
func (s *ServiceContext) IntSnowFlake() error {
	if snowId.Instance != nil {
		s.Snowflake = snowId.Instance
		return nil
	}
	return errors.New("")
}

// Close 关闭所有连接
func (s *ServiceContext) Close() {
	if s.EntClient != nil {
		s.EntClient.Close()
	}
	if s.Redis != nil {
		s.Redis.Close()
	}
}
