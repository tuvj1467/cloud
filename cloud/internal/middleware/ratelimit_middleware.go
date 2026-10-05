package middleware

import (
	"context"
	"net"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"onecloud/internal/pkg/jwt"
)

// 用户级 API 限流（须注册在 JWT 拦截器之前）：
// 已登录（token 验签通过）按 userID 限流；匿名与登录/注册等白名单方法按 IP 限流。
// 身份键不使用 Authorization 原文——客户端可随机化该头使每个请求落入全新限流桶，
// 验签后的 user_id 无法伪造，随机伪造 token 只会落入 IP 桶。
// 匿名 IP 取网关转发的 X-Forwarded-For 首跳（cloud 对端恒为网关，无法区分客户端），
// 可被伪造，仅作尽力而为分桶；生产应置于可信代理后由代理覆写 XFF。
// SendVerificationCode 独立更严阈值（默认每 20s 1 次 ≈ 3/min）。
// 触发返回 gRPC ResourceExhausted，go-zero 网关自动映射为 HTTP 429。

const (
	rlIdleTTL    = 30 * time.Minute // 限流桶空闲回收时间
	rlSweepEvery = 5 * time.Minute  // 空闲桶回收周期
	rlMaxBuckets = 100000           // 桶数硬上限（防随机身份键灌内存），超限后新键共享兜底桶
	rlOverflow   = "__overflow__"   // 超限后新身份键共享的兜底桶键
)

type rateLimitConf struct {
	Enabled       bool
	QPS           float64
	Burst         int
	SendCodeEvery int // 秒
}

// rlEntry 限流桶及其最近使用时间（供空闲回收）
type rlEntry struct {
	lim      *rate.Limiter
	lastUsed time.Time
}

var (
	rlConf      rateLimitConf
	rlMu        sync.Mutex
	rlUser      = map[string]*rlEntry{} // 全局：userID/IP → 限流器
	rlSendC     = map[string]*rlEntry{} // 发送验证码独立桶
	rlSweepOnce sync.Once
)

// SetupRateLimit 注入限流配置（启动时调用一次；未调用则限流关闭）
func SetupRateLimit(enabled bool, qps float64, burst int, sendCodeEvery int) {
	if qps <= 0 {
		qps = 10
	}
	if burst <= 0 {
		burst = 20
	}
	if sendCodeEvery <= 0 {
		sendCodeEvery = 20
	}
	rlMu.Lock()
	rlConf = rateLimitConf{Enabled: enabled, QPS: qps, Burst: burst, SendCodeEvery: sendCodeEvery}
	rlMu.Unlock()

	// 空闲桶周期回收（进程生命周期内只启动一次）
	rlSweepOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(rlSweepEvery)
			defer ticker.Stop()
			for range ticker.C {
				sweepIdle()
			}
		}()
	})
}

// sweepIdle 回收超时未使用的限流桶，防止 map 无限增长
func sweepIdle() {
	cutoff := time.Now().Add(-rlIdleTTL)
	rlMu.Lock()
	defer rlMu.Unlock()
	for _, m := range []map[string]*rlEntry{rlUser, rlSendC} {
		for k, e := range m {
			if e.lastUsed.Before(cutoff) {
				delete(m, k)
			}
		}
	}
}

func rlEnabled() bool {
	rlMu.Lock()
	defer rlMu.Unlock()
	return rlConf.Enabled
}

func rlParams() (qps float64, burst int, sendCodeEvery int) {
	rlMu.Lock()
	defer rlMu.Unlock()
	return rlConf.QPS, rlConf.Burst, rlConf.SendCodeEvery
}

func limiterFor(m map[string]*rlEntry, key string, mk func() *rate.Limiter) *rate.Limiter {
	rlMu.Lock()
	defer rlMu.Unlock()
	now := time.Now()
	if e, ok := m[key]; ok {
		e.lastUsed = now
		return e.lim
	}
	// 超过桶数上限：新身份键降级共享一个兜底桶（随机身份键攻击者互相挤占，
	// 既有正常用户的桶不受影响）
	if len(m) >= rlMaxBuckets {
		if e, ok := m[rlOverflow]; ok {
			e.lastUsed = now
			return e.lim
		}
		e := &rlEntry{lim: mk(), lastUsed: now}
		m[rlOverflow] = e
		return e.lim
	}
	e := &rlEntry{lim: mk(), lastUsed: now}
	m[key] = e
	return e.lim
}

// identityKey 提取限流身份：
// 1) Authorization 验签通过 → user:{userId}（稳定且不可随机化）
// 2) 否则 → ip:{XFF 首跳 | 对端 IP}
func identityKey(ctx context.Context) string {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		tok := ""
		if v := md.Get("gateway-authorization"); len(v) > 0 {
			tok = v[0]
		} else if v := md.Get("authorization"); len(v) > 0 {
			tok = v[0]
		}
		if tok != "" {
			tok = strings.TrimPrefix(tok, "Bearer ")
			if token, claims, err := jwt.ParseToken(tok); err == nil && token != nil && token.Valid && claims.UserId != "" {
				return "user:" + claims.UserId
			}
		}
		// 匿名：优先网关转发的客户端 XFF 首跳
		if v := md.Get("gateway-x-forwarded-for"); len(v) > 0 && v[0] != "" {
			host := strings.TrimSpace(strings.Split(v[0], ",")[0])
			if host != "" {
				return "ip:" + host
			}
		}
	}
	if p, ok := peer.FromContext(ctx); ok && p.Addr != nil {
		// 剥离临时端口：同一 IP 的所有请求共享一个限流桶
		if host, _, err := net.SplitHostPort(p.Addr.String()); err == nil {
			return "ip:" + host
		}
		return "ip:" + p.Addr.String()
	}
	return ""
}

// RateLimitUnaryInterceptor 用户级限流拦截器
func RateLimitUnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		if !rlEnabled() {
			return handler(ctx, req)
		}
		key := identityKey(ctx)
		if key == "" {
			return handler(ctx, req)
		}

		if info.FullMethod == "/storage.User/SendVerificationCode" {
			_, _, every := rlParams()
			l := limiterFor(rlSendC, key, func() *rate.Limiter {
				return rate.NewLimiter(rate.Every(time.Duration(every)*time.Second), 1)
			})
			if !l.Allow() {
				return nil, status.Error(codes.ResourceExhausted, "发送过于频繁，请稍后再试")
			}
			return handler(ctx, req)
		}

		qps, burst, _ := rlParams()
		l := limiterFor(rlUser, key, func() *rate.Limiter {
			return rate.NewLimiter(rate.Limit(qps), burst)
		})
		if !l.Allow() {
			return nil, status.Error(codes.ResourceExhausted, "请求过于频繁，请稍后再试")
		}
		return handler(ctx, req)
	}
}
