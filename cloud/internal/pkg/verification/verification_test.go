package verification

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

// memKV 内存 KV 实现（仅测试用）
type memKV struct {
	mu   sync.Mutex
	data map[string]memEntry
}

type memEntry struct {
	val   string
	expat time.Time
}

func newMemKV() *memKV { return &memKV{data: map[string]memEntry{}} }

func (m *memKV) Get(_ context.Context, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.data[key]
	if !ok || time.Now().After(e.expat) {
		return "", ErrNotFound
	}
	return e.val, nil
}

func (m *memKV) Set(_ context.Context, key, val string, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = memEntry{val: val, expat: time.Now().Add(ttl)}
	return nil
}

func (m *memKV) SetNX(_ context.Context, key, val string, ttl time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e, ok := m.data[key]; ok && time.Now().Before(e.expat) {
		return false, nil
	}
	m.data[key] = memEntry{val: val, expat: time.Now().Add(ttl)}
	return true, nil
}

func (m *memKV) Del(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	return nil
}

func (m *memKV) Incr(_ context.Context, key string, ttl time.Duration) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var n int64
	if e, ok := m.data[key]; ok && time.Now().Before(e.expat) {
		_, _ = fmt.Sscan(e.val, &n)
	}
	n++
	m.data[key] = memEntry{val: fmt.Sprintf("%d", n), expat: time.Now().Add(ttl)}
	return n, nil
}

// noopSender 记录发送的验证码
type noopSender struct{ codes []string }

func (s *noopSender) Send(_ context.Context, _ string, _ int, code string) error {
	s.codes = append(s.codes, code)
	return nil
}

func newTestService() (*Service, *noopSender) {
	sender := &noopSender{}
	return NewService(newMemKV(), sender), sender
}

func TestSendTooFrequent(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()
	if err := svc.Send(ctx, "13800000000", 1); err != nil {
		t.Fatalf("first send: %v", err)
	}
	if err := svc.Send(ctx, "13800000000", 1); !errors.Is(err, ErrTooFrequent) {
		t.Fatalf("want ErrTooFrequent, got %v", err)
	}
	// 不同联系方式不受影响
	if err := svc.Send(ctx, "13900000000", 1); err != nil {
		t.Fatalf("other contact: %v", err)
	}
}

func TestVerifyExpired(t *testing.T) {
	svc, _ := newTestService()
	err := svc.Verify(context.Background(), "13800000000", 1, "123456")
	if !errors.Is(err, ErrCodeExpired) {
		t.Fatalf("want ErrCodeExpired, got %v", err)
	}
}

func TestVerifyWrongThenRight(t *testing.T) {
	svc, sender := newTestService()
	ctx := context.Background()
	if err := svc.Send(ctx, "13800000000", 1); err != nil {
		t.Fatalf("send: %v", err)
	}
	code := sender.codes[0]
	if err := svc.Verify(ctx, "13800000000", 1, "000000"); !errors.Is(err, ErrCodeWrong) {
		t.Fatalf("want ErrCodeWrong, got %v", err)
	}
	// 错 1 次后正确码仍可验证成功
	if err := svc.Verify(ctx, "13800000000", 1, code); err != nil {
		t.Fatalf("verify right: %v", err)
	}
}

func TestVerifyMaxFailInvalidates(t *testing.T) {
	svc, sender := newTestService()
	ctx := context.Background()
	_ = svc.Send(ctx, "13800000000", 1)
	code := sender.codes[0]
	for i := 0; i < 5; i++ {
		_ = svc.Verify(ctx, "13800000000", 1, "000000")
	}
	// 5 次失败后，正确码已被作废
	if err := svc.Verify(ctx, "13800000000", 1, code); !errors.Is(err, ErrCodeExpired) {
		t.Fatalf("want ErrCodeExpired after max fails, got %v", err)
	}
}

func TestVerifyOneTimeConsume(t *testing.T) {
	svc, sender := newTestService()
	ctx := context.Background()
	_ = svc.Send(ctx, "13800000000", 1)
	code := sender.codes[0]
	if err := svc.Verify(ctx, "13800000000", 1, code); err != nil {
		t.Fatalf("first verify: %v", err)
	}
	// 同一码不能重放
	if err := svc.Verify(ctx, "13800000000", 1, code); !errors.Is(err, ErrCodeExpired) {
		t.Fatalf("replay should fail, got %v", err)
	}
}

// TestSendDailyQuota 日级配额：同一联系方式同类型超过 maxSendsPerDay 后拒绝，
// 且不同类型/不同联系方式互不影响；配额满后验证码发送（含真实写码动作）不得执行
func TestSendDailyQuota(t *testing.T) {
	kv := newMemKV()
	svc := NewService(kv, LogSender{})
	ctx := context.Background()

	for i := 1; i <= maxSendsPerDay; i++ {
		// 60s 频率窗口会先拒绝；跳过方式：直接操作 kv 清掉 limit 键模拟窗口过期
		_ = kv.Del(ctx, limitKey("13800000001", 1))
		if err := svc.Send(ctx, "13800000001", 1); err != nil {
			t.Fatalf("第 %d 次发送不应失败: %v", i, err)
		}
	}
	// 清掉频率窗口后再次发送：应命中日配额
	_ = kv.Del(ctx, limitKey("13800000001", 1))
	if err := svc.Send(ctx, "13800000001", 1); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("超过日配额应返回 ErrQuotaExceeded，实际: %v", err)
	}
	// 其他类型不受影响
	if err := svc.Send(ctx, "13800000001", 3); err != nil {
		t.Fatalf("其他类型不应受配额影响: %v", err)
	}
	// 其他联系方式不受影响
	if err := svc.Send(ctx, "13900000000", 1); err != nil {
		t.Fatalf("其他联系方式不应受配额影响: %v", err)
	}
}
