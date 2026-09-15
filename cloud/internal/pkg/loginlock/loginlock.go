// Package loginlock 登录失败锁定：同账号连续失败达阈值后锁定一段时间。
// Redis 计数（多实例共享）；Redis 不可用时跳过（可用性优先）。
package loginlock

import (
	"context"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	FailThreshold = 5                // 连续失败阈值
	FailWindow    = 15 * time.Minute // 失败计数窗口
	LockDuration  = 15 * time.Minute // 锁定时长
)

var (
	mu      sync.Mutex
	rdb     *redis.Client
	memFail = map[string]*memEntry{}
	memLock = map[string]time.Time{}
)

type memEntry struct {
	n   int64
	exp time.Time
}

// Setup 注入 Redis 客户端（nil 则使用进程内回退，仅单实例有效）
func Setup(client *redis.Client) {
	mu.Lock()
	rdb = client
	mu.Unlock()
}

func failKey(account string) string { return "login_fail:" + account }
func lockKey(account string) string { return "login_lock:" + account }

// Locked 判断账号是否处于锁定期，返回是否锁定与剩余时长
func Locked(ctx context.Context, account string) (bool, time.Duration) {
	if rdb != nil {
		ttl, err := rdb.TTL(ctx, lockKey(account)).Result()
		if err == nil && ttl > 0 {
			return true, ttl
		}
		return false, 0
	}
	mu.Lock()
	deadline, ok := memLock[account]
	mu.Unlock()
	if !ok || time.Now().After(deadline) {
		return false, 0
	}
	return true, time.Until(deadline)
}

// RecordFail 记录一次登录失败；达到阈值返回 locked=true
func RecordFail(ctx context.Context, account string) (locked bool) {
	mu.Lock()
	client := rdb
	mu.Unlock()

	if client != nil {
		n, err := client.Incr(ctx, failKey(account)).Result()
		if err != nil {
			return false // Redis 故障时放行（可用性优先）
		}
		if n == 1 {
			client.Expire(ctx, failKey(account), FailWindow)
		}
		if n >= FailThreshold {
			client.SetNX(ctx, lockKey(account), 1, LockDuration)
			client.Del(ctx, failKey(account))
			return true
		}
		return false
	}

	mu.Lock()
	defer mu.Unlock()
	e, ok := memFail[account]
	if !ok || time.Now().After(e.exp) {
		e = &memEntry{exp: time.Now().Add(FailWindow)}
		memFail[account] = e
	}
	e.n++
	if e.n >= FailThreshold {
		memLock[account] = time.Now().Add(LockDuration)
		delete(memFail, account)
		return true
	}
	return false
}

// Clear 登录成功后清除失败计数
func Clear(ctx context.Context, account string) {
	mu.Lock()
	client := rdb
	mu.Unlock()
	if client != nil {
		client.Del(ctx, failKey(account))
		return
	}
	mu.Lock()
	delete(memFail, account)
	mu.Unlock()
}
