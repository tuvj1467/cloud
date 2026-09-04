// Package migrate 存量数据迁移工具
// 启动时一次性执行，幂等
package migrate

import (
	"context"
	"fmt"

	"onecloud/ent"
	"onecloud/ent/bucket"
	"onecloud/ent/file"
	"onecloud/internal/pkg/snowId"
)

// MigrateFileBucket 迁移 bucket_id 为空的 File 记录到 default 桶
//
// 策略：
//  1. 查询所有 bucket_id 为空字符串的 File 记录，按 user_id 分组
//  2. 每个用户：确保有 default 桶（不存在则创建）
//  3. 回填 file.bucket_id，累加桶已用容量
//
// 幂等性：查询条件限定 bucket_id == ""，重复执行不会重复回填
func MigrateFileBucket(ctx context.Context, client *ent.Client) error {
	// 1. 查询所有 bucket_id 为空的文件
	files, err := client.File.Query().
		Where(file.BucketIDEQ("")).
		All(ctx)
	if err != nil {
		return fmt.Errorf("查询待迁移文件失败: %w", err)
	}
	if len(files) == 0 {
		return nil // 无需迁移
	}
	fmt.Printf("[migrate] 发现 %d 条 bucket_id 为空的 File 记录，开始迁移...\n", len(files))

	// 2. 按 user_id 分组
	userFiles := make(map[string][]*ent.File)
	for _, f := range files {
		userFiles[f.UserID] = append(userFiles[f.UserID], f)
	}

	// 3. 逐用户处理
	totalMigrated := 0
	for userID, fs := range userFiles {
		// 3.1 确保有 default 桶
		b, err := ensureDefaultBucket(ctx, client, userID)
		if err != nil {
			fmt.Printf("[migrate] 用户 %s 创建 default 桶失败: %v\n", userID, err)
			continue
		}

		// 3.2 回填每个文件的 bucket_id
		var totalSize int64
		for _, f := range fs {
			_, err := client.File.UpdateOneID(f.ID).
				SetBucketID(b.ID).
				Save(ctx)
			if err != nil {
				fmt.Printf("[migrate] 文件 %s 回填 bucket_id 失败: %v\n", f.ID, err)
				continue
			}
			totalSize += f.Size
			totalMigrated++
		}

		// 3.3 累加桶已用容量（直接加，绕过乐观锁，迁移场景不需要并发保护）
		if totalSize > 0 {
			_, _ = client.Bucket.UpdateOneID(b.ID).
				AddUsedSize(totalSize).
				Save(ctx)
		}

		fmt.Printf("[migrate] 用户 %s 迁移完成：default 桶=%s，文件数=%d，总大小=%d\n",
			userID, b.ID, len(fs), totalSize)
	}

	fmt.Printf("[migrate] 迁移完成，共处理 %d 条记录\n", totalMigrated)
	return nil
}

// ensureDefaultBucket 获取或创建用户的 default 桶
func ensureDefaultBucket(ctx context.Context, client *ent.Client, userID string) (*ent.Bucket, error) {
	// 先查
	b, err := client.Bucket.Query().
		Where(
			bucket.UserIDEQ(userID),
			bucket.NameEQ("default"),
		).
		First(ctx)
	if err == nil {
		return b, nil
	}
	if !ent.IsNotFound(err) {
		return nil, fmt.Errorf("查询 default 桶失败: %w", err)
	}

	// 不存在则创建
	if snowId.Instance == nil {
		return nil, fmt.Errorf("雪花 ID 生成器未初始化")
	}
	bucketID := snowId.Instance.MustIdS()
	return client.Bucket.Create().
		SetID(bucketID).
		SetName("default").
		SetUserID(userID).
		SetSizeLimit(1024 * 1024 * 1024). // 默认 1GB
		Save(ctx)
}
