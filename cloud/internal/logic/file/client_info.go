package filelogic

import (
	"context"

	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
)

// clientInfo 提取审计来源：真实客户端 IP（X-Forwarded-For 优先，回退 gRPC 对端地址）与 UA
func clientInfo(ctx context.Context) (ip, userAgent string) {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if v := md.Get("gateway-x-forwarded-for"); len(v) > 0 && v[0] != "" {
			ip = v[0]
		}
		if v := md.Get("gateway-user-agent"); len(v) > 0 && v[0] != "" {
			userAgent = v[0]
		}
	}
	if ip == "" {
		if p, ok := peer.FromContext(ctx); ok && p.Addr != nil {
			ip = p.Addr.String()
		}
	}
	return ip, userAgent
}
