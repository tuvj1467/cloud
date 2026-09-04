// Package cache 提供 Redis 缓存辅助
// multipart_cache.go：分片上传状态缓存
package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// MultipartMeta 分片上传元信息（Redis 中存储）
type MultipartMeta struct {
	UploadID   string `json:"upload_id"`   // 上传唯一标识
	UserID     string `json:"user_id"`     // 上传用户
	BucketID   string `json:"bucket_id"`   // 目标桶
	FileName   string `json:"file_name"`   // 目标文件名
	FileSuffix string `json:"file_suffix"` // 目标文件后缀
	FileSize   int64  `json:"file_size"`   // 文件总大小
	TotalParts int    `json:"total_parts"` // 分片总数
	StorageID  string `json:"storage_id"`  // 存储驱动返回的物理 upload_id
}

// MultipartCache 分片上传状态缓存
// Key1: upload:{upload_id}:meta   -> JSON 元信息
// Key2: upload:{upload_id}:parts  -> hash, field=part_number, value=part_md5
type MultipartCache struct {
	client *redis.Client
	ttl    time.Duration
}

// NewMultipartCache 创建分片上传缓存实例
func NewMultipartCache(client *redis.Client) *MultipartCache {
	return &MultipartCache{
		client: client,
		ttl:    24 * time.Hour, // 默认 24 小时
	}
}

// metaKey 元信息键
func (c *MultipartCache) metaKey(uploadID string) string {
	return fmt.Sprintf("upload:%s:meta", uploadID)
}

// partsKey 分片状态键
func (c *MultipartCache) partsKey(uploadID string) string {
	return fmt.Sprintf("upload:%s:parts", uploadID)
}

// SetMeta 写入上传元信息
func (c *MultipartCache) SetMeta(ctx context.Context, meta *MultipartMeta) error {
	if c.client == nil {
		return nil
	}
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return c.client.Set(ctx, c.metaKey(meta.UploadID), data, c.ttl).Err()
}

// GetMeta 读取上传元信息
// 未命中返回 nil 与 nil error
func (c *MultipartCache) GetMeta(ctx context.Context, uploadID string) (*MultipartMeta, error) {
	if c.client == nil {
		return nil, nil
	}
	val, err := c.client.Get(ctx, c.metaKey(uploadID)).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var meta MultipartMeta
	if err := json.Unmarshal([]byte(val), &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

// AddPart 记录已上传分片（HSetNX 原子写入，已存在则跳过）
// 返回 true 表示本次新写入，false 表示已存在（断点续传幂等命中）
func (c *MultipartCache) AddPart(ctx context.Context, uploadID string, partNumber int, partMD5 string) (bool, error) {
	if c.client == nil {
		return true, nil
	}
	field := fmt.Sprintf("%d", partNumber)
	res, err := c.client.HSetNX(ctx, c.partsKey(uploadID), field, partMD5).Result()
	if err != nil {
		return false, err
	}
	// 续期
	_ = c.client.Expire(ctx, c.partsKey(uploadID), c.ttl).Err()
	return res, nil
}

// GetPart 查询单个分片是否已上传
// 返回 part_md5 与是否命中
func (c *MultipartCache) GetPart(ctx context.Context, uploadID string, partNumber int) (string, bool, error) {
	if c.client == nil {
		return "", false, nil
	}
	field := fmt.Sprintf("%d", partNumber)
	val, err := c.client.HGet(ctx, c.partsKey(uploadID), field).Result()
	if err == redis.Nil {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return val, true, nil
}

// CountParts 统计已上传分片数
func (c *MultipartCache) CountParts(ctx context.Context, uploadID string) (int, error) {
	if c.client == nil {
		return 0, nil
	}
	cnt, err := c.client.HLen(ctx, c.partsKey(uploadID)).Result()
	return int(cnt), err
}

// Clear 清除某次上传的所有 Redis 状态
func (c *MultipartCache) Clear(ctx context.Context, uploadID string) error {
	if c.client == nil {
		return nil
	}
	pipe := c.client.Pipeline()
	pipe.Del(ctx, c.metaKey(uploadID))
	pipe.Del(ctx, c.partsKey(uploadID))
	_, err := pipe.Exec(ctx)
	return err
}
