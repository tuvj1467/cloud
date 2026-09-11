package middleware

import (
	"context"
	"net"
	"sync"
	"time"

	"golang.org/x/time/rate"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// 用户级 API 限流（须注册在 JWT 拦截器之前）：
// 已登录按 userID 限流；匿名与登录/注册等白名单方法按对端 IP 限流；
// SendVerificationCode 独立更严阈值（默认每 20s 1 次 ≈ 3/min）。
// 触发返回 gRPC ResourceExhausted，go-zero 网关自动映射为 HTTP 429。

type rateLimitConf struct {
	Enabled       bool
	QPS           float64
	Burst         int
	SendCodeEvery int // 秒
}

var (
	rlConf  rateLimitConf
	rlMu    sync.Mutex
	rlUser  = map[string]*rate.Limiter{} // 全局：userID/token/IP → 限流器
	rlSendC = map[string]*rate.Limiter{} // 发送验证码独立桶
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

func limiterFor(m map[string]*rate.Limiter, key string, mk func() *rate.Limiter) *rate.Limiter {
	rlMu.Lock()
	defer rlMu.Unlock()
	l, ok := m[key]
	if !ok {
		l = mk()
		m[key] = l
	}
	return l
}

// identityKey 提取限流身份：Authorization token 优先，回退对端 IP
func identityKey(ctx context.Context) string {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if v := md.Get("gateway-authorization"); len(v) > 0 && v[0] != "" {
			return "tok:" + v[0]
		}
		if v := md.Get("authorization"); len(v) > 0 && v[0] != "" {
			return "tok:" + v[0]
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
