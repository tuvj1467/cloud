package database

import (
	"context"
	"fmt"
	"onecloud/ent"

	"entgo.io/ent/dialect"
	_ "github.com/go-sql-driver/mysql"
)

type DBConfig struct {
	Host            string
	User            string
	Password        string
	Database        string
	Charset         string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime int
	ConnMaxIdleTime int
	Debug           bool
	Loc             string
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
