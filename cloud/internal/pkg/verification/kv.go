// Package verification 提供验证码的生成、发送与校验
// key 规范唯一定义处，禁止在组件外拼装验证码相关 Redis key
package verification

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound 目标 key 不存在（未生成或已过期）
var ErrNotFound = errors.New("verification: key 不存在")

// ErrUnavailable KV 后端不可用（如未配置 Redis）
var ErrUnavailable = errors.New("verification: 存储后端不可用")

// KV 验证码存储的最小抽象：生产用 Redis 实现（cache.RedisKV），测试用内存实现
type KV interface {
	// Get 读取，未命中返回 ErrNotFound
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key, val string, ttl time.Duration) error
	SetNX(ctx context.Context, key, val string, ttl time.Duration) (bool, error)
	Del(ctx context.Context, key string) error
	// Incr 自增计数，首次自增（结果为 1）时设置过期时间
	Incr(ctx context.Context, key string, ttl time.Duration) (int64, error)
}
