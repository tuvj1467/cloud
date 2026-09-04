// Package cache 提供 Redis 缓存辅助
// md5_cache.go：秒传索引（MD5 → file_id）缓存
package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// MD5Cache 秒传索引缓存
// Key: md5:{user_id}:{md5}  Value: file_id
type MD5Cache struct {
	client *redis.Client
	ttl    time.Duration
}

// NewMD5Cache 创建秒传索引缓存实例
func NewMD5Cache(client *redis.Client) *MD5Cache {
	return &MD5Cache{
		client: client,
		ttl:    30 * 24 * time.Hour, // 默认 30 天
	}
}

// key 拼接 Redis 键
func (c *MD5Cache) key(userID, md5 string) string {
	return fmt.Sprintf("md5:%s:%s", userID, md5)
}

// Get 查询 MD5 索引对应的 file_id
// 未命中返回空串与 nil error
func (c *MD5Cache) Get(ctx context.Context, userID, md5 string) (string, error) {
	if c.client == nil {
		return "", nil
	}
	val, err := c.client.Get(ctx, c.key(userID, md5)).Result()
	if err == redis.Nil {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return val, nil
}

// Set 写入 MD5 索引
func (c *MD5Cache) Set(ctx context.Context, userID, md5, fileID string) error {
	if c.client == nil {
		return nil
	}
	return c.client.Set(ctx, c.key(userID, md5), fileID, c.ttl).Err()
}

// Del 删除 MD5 索引
// 仅当 Redis 中 value 与 expectedFileID 相同时才删除，避免覆盖竞态
func (c *MD5Cache) Del(ctx context.Context, userID, md5, expectedFileID string) error {
	if c.client == nil {
		return nil
	}
	// 用 Lua 脚本保证「比较 + 删除」原子性
	const script = `
		if redis.call('get', KEYS[1]) == ARGV[1] then
			return redis.call('del', KEYS[1])
		else
			return 0
		end
	`
	return c.client.Eval(ctx, script, []string{c.key(userID, md5)}, expectedFileID).Err()
}
