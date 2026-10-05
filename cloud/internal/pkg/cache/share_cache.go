// Package cache 提供 Redis 缓存辅助
// share_cache.go：文件分享信息缓存
package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// ShareCache 文件分享缓存
// Key1: share:{code}:meta    -> JSON 元信息（不可变部分）
// Key2: share:{code}:counter -> 已用访问次数（INCR 原子递增）
type ShareCache struct {
	client *redis.Client
}

// NewShareCache 创建分享缓存实例
func NewShareCache(client *redis.Client) *ShareCache {
	return &ShareCache{client: client}
}

// ShareData 缓存中存储的分享元信息（不含已用次数，已用次数独立计数）
type ShareData struct {
	ID         string     `json:"id"`
	FileID     string     `json:"file_id"`
	UserID     string     `json:"user_id"`
	ShareCode  string     `json:"share_code"`
	Permission string     `json:"permission"`
	ExpireAt   *time.Time `json:"expire_at,omitempty"`
	MaxCount   *int       `json:"max_count,omitempty"`
}

func (c *ShareCache) metaKey(code string) string {
	return fmt.Sprintf("share:%s:meta", code)
}

func (c *ShareCache) counterKey(code string) string {
	return fmt.Sprintf("share:%s:counter", code)
}

// ttl 根据过期时间计算缓存 TTL；永久返回 7 天兜底
func (c *ShareCache) ttl(data *ShareData) time.Duration {
	if data.ExpireAt == nil {
		return 7 * 24 * time.Hour
	}
	d := time.Until(*data.ExpireAt)
	if d <= 0 {
		return 0
	}
	return d
}

// Set 写入分享元信息（不含已用次数）
func (c *ShareCache) Set(ctx context.Context, data *ShareData, usedCount int) error {
	if c.client == nil {
		return nil
	}
	ttl := c.ttl(data)
	if ttl == 0 {
		return nil // 已过期不写
	}
	buf, err := json.Marshal(data)
	if err != nil {
		return err
	}
	pipe := c.client.Pipeline()
	pipe.Set(ctx, c.metaKey(data.ShareCode), buf, ttl)
	pipe.Set(ctx, c.counterKey(data.ShareCode), usedCount, ttl)
	_, err = pipe.Exec(ctx)
	return err
}

// GetMeta 读取分享元信息（不含已用次数）
func (c *ShareCache) GetMeta(ctx context.Context, code string) (*ShareData, error) {
	if c.client == nil {
		return nil, nil
	}
	val, err := c.client.Get(ctx, c.metaKey(code)).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var data ShareData
	if err := json.Unmarshal([]byte(val), &data); err != nil {
		return nil, err
	}
	return &data, nil
}

// GetUsedCount 读取已用次数
func (c *ShareCache) GetUsedCount(ctx context.Context, code string) (int, error) {
	if c.client == nil {
		return 0, nil
	}
	val, err := c.client.Get(ctx, c.counterKey(code)).Int()
	if err == redis.Nil {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return val, nil
}

// IncrUsedCount 原子递增已用次数，返回递增后的新值
func (c *ShareCache) IncrUsedCount(ctx context.Context, code string) (int, error) {
	if c.client == nil {
		return 0, nil
	}
	val, err := c.client.Incr(ctx, c.counterKey(code)).Result()
	if err != nil {
		return 0, err
	}
	return int(val), nil
}

// Del 清除分享缓存（元信息 + 计数器）
func (c *ShareCache) Del(ctx context.Context, code string) error {
	if c.client == nil {
		return nil
	}
	pipe := c.client.Pipeline()
	pipe.Del(ctx, c.metaKey(code))
	pipe.Del(ctx, c.counterKey(code))
	_, err := pipe.Exec(ctx)
	return err
}

// allowDownloadScript 原子的「校验并递增」下载计数：
// max<=0 表示不限次；已达上限返回 0（不递增），否则递增并返回新值。
// 首次递增（或键无 TTL——被内存淘汰后重建）时以 meta 同步的 TTL 补过期，
// 防止 counter 键被淘汰重建后从 0 重新计数且永不过期（已用尽的下载次数被重置）
var allowDownloadScript = redis.NewScript(`
local cur = tonumber(redis.call('GET', KEYS[1]) or '0')
local max = tonumber(ARGV[1])
if max > 0 and cur >= max then
  return 0
end
local n = redis.call('INCR', KEYS[1])
if redis.call('TTL', KEYS[1]) < 0 and tonumber(ARGV[2]) > 0 then
  redis.call('EXPIRE', KEYS[1], ARGV[2])
end
return n
`)

// AllowDownload 原子校验并递增下载次数（maxCount/TTL 均从分享元信息推导）
// maxCount<=0 不限次；返回 allowed=false 表示已达上限（此时 newCount 无意义）
func (c *ShareCache) AllowDownload(ctx context.Context, data *ShareData) (allowed bool, newCount int, err error) {
	if c.client == nil {
		return true, 0, nil
	}
	maxCount := 0
	if data.MaxCount != nil {
		maxCount = int(*data.MaxCount)
	}
	ttl := int(c.ttl(data).Seconds())
	res, err := allowDownloadScript.Run(ctx, c.client, []string{c.counterKey(data.ShareCode)}, maxCount, ttl).Int()
	if err != nil {
		// Redis 故障时放行（可用性优先），由 DB 侧 used_count 兜底对账
		return true, 0, err
	}
	if res == 0 {
		return false, 0, nil
	}
	return true, res, nil
}
