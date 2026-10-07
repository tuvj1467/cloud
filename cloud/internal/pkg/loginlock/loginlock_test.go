package loginlock

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// 内存路径（未配置 Redis）与 Redis 故障降级路径测试

func resetState() {
	mu.Lock()
	memFail = map[string]*memEntry{}
	memLock = map[string]time.Time{}
	mu.Unlock()
}

func TestMemoryPathLockoutFlow(t *testing.T) {
	Setup(nil) // 未配置 Redis：进程内计数
	resetState()
	ctx := context.Background()

	for i := 1; i < FailThreshold; i++ {
		if RecordFail(ctx, "acc-1") {
			t.Fatalf("第 %d 次失败不应锁定", i)
		}
	}
	if !RecordFail(ctx, "acc-1") {
		t.Fatal("达到阈值应锁定")
	}
	if locked, remain := Locked(ctx, "acc-1"); !locked || remain <= 0 {
		t.Fatalf("锁定期内 Locked 应为 true: %v %v", locked, remain)
	}
	// 其他账号不受影响
	if locked, _ := Locked(ctx, "acc-2"); locked {
		t.Fatal("其他账号不应被连带锁定")
	}
	// Clear 清除的是失败计数；活动锁定按设计仅随 TTL 过期（登录被拒时不会走到 Clear）
	if locked, _ := Locked(ctx, "acc-1"); !locked {
		t.Fatal("活动锁定不应被 Clear 清除（仅 TTL 过期）")
	}
}

func TestClearResetsFailCounter(t *testing.T) {
	Setup(nil)
	resetState()
	ctx := context.Background()

	// 累计到阈值-1 后 Clear，再累计阈值-1 次：计数已重置则不应锁定
	for i := 0; i < FailThreshold-1; i++ {
		RecordFail(ctx, "acc-3")
	}
	Clear(ctx, "acc-3")
	for i := 0; i < FailThreshold-1; i++ {
		if RecordFail(ctx, "acc-3") {
			t.Fatal("Clear 后失败计数应重置，不应达到阈值")
		}
	}
	if locked, _ := Locked(ctx, "acc-3"); locked {
		t.Fatal("计数重置后不应锁定")
	}
	// 再失败一次即达阈值
	if !RecordFail(ctx, "acc-3") {
		t.Fatal("重置后再累计阈值次应锁定")
	}
}

func TestMemoryFailWindowExpiry(t *testing.T) {
	Setup(nil)
	resetState()
	ctx := context.Background()
	for i := 0; i < FailThreshold-1; i++ {
		RecordFail(ctx, "acc-w")
	}
	// 计数窗口拨到过期之后：下次失败重新计数
	mu.Lock()
	for _, e := range memFail {
		e.exp = time.Now().Add(-time.Minute)
	}
	mu.Unlock()
	if RecordFail(ctx, "acc-w") {
		t.Fatal("窗口过期后应重新计数而非锁定")
	}
}

func TestRedisFailureDegradesToMemory(t *testing.T) {
	// 指向必然拒绝连接的地址，模拟「已配置但故障」
	bad := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", DialTimeout: 200 * time.Millisecond, ReadTimeout: 200 * time.Millisecond, WriteTimeout: 200 * time.Millisecond})
	Setup(bad)
	defer Setup(nil)
	resetState()
	ctx := context.Background()

	for i := 1; i < FailThreshold; i++ {
		if RecordFail(ctx, "deg-1") {
			t.Fatalf("降级第 %d 次失败不应锁定", i)
		}
	}
	if !RecordFail(ctx, "deg-1") {
		t.Fatal("降级路径达到阈值也应锁定（而非 fail-open 放行）")
	}
	if locked, _ := Locked(ctx, "deg-1"); !locked {
		t.Fatal("降级路径 Locked 应读取进程内锁定状态")
	}
}
