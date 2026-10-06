package middleware

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	"golang.org/x/time/rate"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"

	"onecloud/internal/pkg/jwt"
)

// 限流桶身份键与容量上限测试（纯内存，无 Redis 依赖）

func newLimiter() *rate.Limiter { return rate.NewLimiter(rate.Limit(1), 1) }

func TestLimiterForCapFallsBackToOverflowBucket(t *testing.T) {
	m := map[string]*rlEntry{}
	// 灌满 rlMaxBuckets 个桶
	for i := 0; i < rlMaxBuckets; i++ {
		limiterFor(m, "key-"+strconv.Itoa(i), newLimiter)
	}
	if len(m) != rlMaxBuckets {
		t.Fatalf("桶数应为 %d，实际 %d", rlMaxBuckets, len(m))
	}
	// 超限的新键必须落到共享兜底桶，map 不再增长
	first := limiterFor(m, "intruder-1", newLimiter)
	second := limiterFor(m, "intruder-2", newLimiter)
	if len(m) != rlMaxBuckets+1 {
		t.Fatalf("超限后 map 不应继续增长：%d", len(m))
	}
	if first != second {
		t.Fatal("超限后的新键应共享同一个兜底桶")
	}
	// 兜底桶命中也要刷新 lastUsed（防被 sweep 误回收）
	rlMu.Lock()
	fresh := time.Now().Add(-time.Minute)
	stale := m[rlOverflow].lastUsed.After(fresh)
	rlMu.Unlock()
	if !stale {
		t.Fatal("兜底桶命中应刷新 lastUsed")
	}
}

func TestSweepIdleRemovesExpiredBuckets(t *testing.T) {
	// sweepIdle 扫描的是全局桶，测试直接操作 rlUser（用唯一键名避免用例间污染）
	limiterFor(rlUser, "sweep-test-old", newLimiter)
	limiterFor(rlUser, "sweep-test-fresh", newLimiter)
	rlMu.Lock()
	rlUser["sweep-test-old"].lastUsed = time.Now().Add(-rlIdleTTL - time.Minute)
	rlMu.Unlock()

	sweepIdle()
	rlMu.Lock()
	_, oldAlive := rlUser["sweep-test-old"]
	_, freshAlive := rlUser["sweep-test-fresh"]
	delete(rlUser, "sweep-test-fresh")
	rlMu.Unlock()
	if oldAlive {
		t.Fatal("超期桶应被回收")
	}
	if !freshAlive {
		t.Fatal("活跃桶不应被回收")
	}
}

func TestIdentityKeyFromVerifiedToken(t *testing.T) {
	jwt.Init("identity-key-test-secret-0123456789", 3600, "t")
	token, err := jwt.GenerateToken("user-abc", 1)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	md := metadata.Pairs("gateway-authorization", "Bearer "+token)
	ctx := metadata.NewIncomingContext(context.Background(), md)
	key := identityKey(ctx)
	if key != "user:user-abc" {
		t.Fatalf("验签通过的 token 应产生 user 键，实际 %q", key)
	}
}

func TestIdentityKeyRandomTokenFallsToIP(t *testing.T) {
	// 伪造随机 Authorization：验签失败必须回落 IP 桶（身份不可伪造）
	md := metadata.Pairs("gateway-authorization", "Bearer random-garbage")
	ctx := peer.NewContext(metadata.NewIncomingContext(context.Background(), md), peerOf("10.1.2.3", 55555))
	if key := identityKey(ctx); key != "ip:10.1.2.3" {
		t.Fatalf("伪造 token 应回落 IP 键，实际 %q", key)
	}
}

func TestIdentityKeyXFFPriority(t *testing.T) {
	md := metadata.Pairs("gateway-x-forwarded-for", "203.0.113.7, 10.0.0.1")
	ctx := peer.NewContext(metadata.NewIncomingContext(context.Background(), md), peerOf("10.1.2.3", 55555))
	if key := identityKey(ctx); key != "ip:203.0.113.7" {
		t.Fatalf("XFF 首跳应优先，实际 %q", key)
	}
}

func TestIdentityKeyPeerIPStripsPort(t *testing.T) {
	ctx := peer.NewContext(context.Background(), peerOf("192.168.1.9", 12345))
	if key := identityKey(ctx); key != "ip:192.168.1.9" {
		t.Fatalf("对端 IP 应剥离端口，实际 %q", key)
	}
}

func peerOf(ip string, port int) *peer.Peer {
	return &peer.Peer{Addr: &net.TCPAddr{IP: net.ParseIP(ip), Port: port}}
}
