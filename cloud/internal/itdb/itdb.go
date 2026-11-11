// Package itdb 数据库集成测试共享基建（原 repository/it_test.go 内联实现抽出）。
//
// 供多个包的 DB_IT 用例复用（repository、cleanup 等）：独立测试库 cloud_it 自动创建、
// 表结构自动迁移、每次调用清空全部表。DB_IT!=1 时一律 Skip，常规 go test / CI 不受影响。
// 注意使用【独立的测试库】（默认 cloud_it），勿指向业务库 cloud。
package itdb

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/go-sql-driver/mysql"

	"onecloud/ent"
	"onecloud/ent/bucket"
)

const itDBName = "cloud_it"

// Client 返回指向独立测试库的 ent 客户端（表结构自动迁移，数据在每次调用时清空）
func Client(t *testing.T) *ent.Client {
	t.Helper()
	if os.Getenv("DB_IT") != "1" {
		t.Skip("数据库集成测试未启用：设置 DB_IT=1 运行（使用独立测试库 cloud_it）")
	}
	dsn := os.Getenv("DB_IT_DSN")
	if dsn == "" {
		dsn = "root:12345@tcp(127.0.0.1:3306)/" + itDBName + "?charset=utf8mb4&parseTime=True&loc=Local"
	}

	ctx := context.Background()
	// 先不指定库连接，确保测试库存在
	adminDSN := strings.Replace(dsn, "/"+itDBName+"?", "/?", 1)
	admin, err := sql.Open("mysql", adminDSN)
	if err != nil {
		t.Fatalf("连接 MySQL 失败: %v", err)
	}
	defer admin.Close()
	if _, err := admin.ExecContext(ctx,
		"CREATE DATABASE IF NOT EXISTS `"+itDBName+"` DEFAULT CHARSET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		t.Fatalf("创建测试库失败: %v", err)
	}

	dbConn, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("连接测试库失败: %v", err)
	}
	dbConn.SetMaxOpenConns(60)
	client := ent.NewClient(ent.Driver(entsql.OpenDB("mysql", dbConn)))
	t.Cleanup(func() { client.Close() })

	if err := client.Schema.Create(ctx); err != nil {
		t.Fatalf("自动迁移失败: %v", err)
	}
	// 清空全部表（无跨实体外键，直接 TRUNCATE 即可）
	for _, table := range []string{"users", "buckets", "files", "file_parts", "file_shares",
		"share_access_logs", "admin_audit_logs", "storage_snapshots"} {
		if _, err := dbConn.ExecContext(ctx, "TRUNCATE TABLE `"+table+"`"); err != nil {
			t.Fatalf("清空表 %s 失败（可忽略首次运行的建表时序）: %v", table, err)
		}
	}
	return client
}

// Bucket 建一个指定容量的测试桶（归属 it-user）
func Bucket(t *testing.T, client *ent.Client, id string, sizeLimit int64) *ent.Bucket {
	t.Helper()
	b, err := client.Bucket.Create().
		SetID(id).
		SetName("it-bucket-" + id).
		SetUserID("it-user").
		SetSizeLimit(sizeLimit).
		SetVisibility(bucket.VisibilityPrivate).
		Save(context.Background())
	if err != nil {
		t.Fatalf("创建测试桶失败: %v", err)
	}
	return b
}
