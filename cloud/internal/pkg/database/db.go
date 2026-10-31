package database

import (
	"context"
	"fmt"
	"onecloud/ent"

	"entgo.io/ent/dialect"
	_ "github.com/go-sql-driver/mysql"
)

// DBConfig MySQL 连接配置（由 config.DB 逐字段转换而来，字段语义见 config.DB 注释）
// 独立定义的原因：database 包不反向依赖 internal/config，便于单独复用/测试
type DBConfig struct {
	Host            string // 连接地址（IP:端口）
	User            string // 用户名
	Password        string // 密码
	Database        string // 库名
	Charset         string // 字符集（utf8mb4 支持 emoji）
	MaxOpenConns    int    // 最大打开连接数（0=驱动默认）
	MaxIdleConns    int    // 最大空闲连接数
	ConnMaxLifetime int    // 连接最大存活时间（秒）
	ConnMaxIdleTime int    // 连接最大空闲时间（秒）
	Debug           bool   // 打印 SQL（生产必须 false，日志含用户数据）
	Loc             string // time.Time 解析时区（parseTime 用），Local=跟随系统
}

func ConnectMySQL(config DBConfig) (*ent.Client, error) {
	// 构建 DSN
	dsn := fmt.Sprintf("%s:%s@tcp(%s)/%s?charset=%s&parseTime=True&loc=%s",
		config.User,
		config.Password,
		config.Host,
		config.Database,
		config.Charset,
		config.Loc,
	)

	// 初始化 ent 客户端
	client, err := ent.Open(dialect.MySQL, dsn)
	if err != nil {
		return nil, fmt.Errorf("failed opening connection to mysql: %v", err)
	}

	return client, nil
}

func AutoMigrate(client *ent.Client) error {
	// 自动迁移 schema
	if err := client.Schema.Create(context.Background()); err != nil {
		return fmt.Errorf("failed creating schema resources: %v", err)
	}
	return nil
}
