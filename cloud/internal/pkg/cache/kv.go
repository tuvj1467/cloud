package cache

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"

	"onecloud/internal/pkg/verification"
)

// RedisKV 将 *redis.Client 适配为 verification.KV
// client 为 nil 时所有方法返回 verification.ErrUnavailable（Redis 未配置的服务降级场景）
type RedisKV struct {
	client *redis.Client
}

// NewRedisKV 创建 Redis 适配器
func NewRedisKV(client *redis.Client) *RedisKV {
	return &RedisKV{client: client}
}

func (a *RedisKV) Get(ctx context.Context, key string) (string, error) {
	if a.client == nil {
		return "", verification.ErrUnavailable
	}
	val, err := a.client.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return "", verification.ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return val, nil
}

func (a *RedisKV) Set(ctx context.Context, key, val string, ttl time.Duration) error {
	if a.client == nil {
		return verification.ErrUnavailable
	}
	return a.client.Set(ctx, key, val, ttl).Err()
}

func (a *RedisKV) SetNX(ctx context.Context, key, val string, ttl time.Duration) (bool, error) {
	if a.client == nil {
		return false, verification.ErrUnavailable
	}
	return a.client.SetNX(ctx, key, val, ttl).Result()
}

func (a *RedisKV) Del(ctx context.Context, key string) error {
	if a.client == nil {
		return verification.ErrUnavailable
	}
	return a.client.Del(ctx, key).Err()
}

// incrWithTTL 自增并在需要时设置过期（Lua 原子）：
// 首次自增（n==1）或键无 TTL（历史非原子写入遗留的永久键）时补 TTL，
// 消除「INCR 成功但 EXPIRE 失败/进程崩溃」留下永久 key 导致的计数永久锁定
var incrWithTTL = redis.NewScript(`
local n = redis.call('INCR', KEYS[1])
if n == 1 or redis.call('TTL', KEYS[1]) < 0 then
  redis.call('EXPIRE', KEYS[1], ARGV[1])
end
return n
`)

// Incr 自增计数；首次自增或键缺失 TTL 时（原子）设置过期时间
func (a *RedisKV) Incr(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	if a.client == nil {
		return 0, verification.ErrUnavailable
	}
	res, err := incrWithTTL.Run(ctx, a.client, []string{key}, int(ttl.Seconds())).Int64()
	if err != nil {
		return 0, err
	}
	return res, nil
}
