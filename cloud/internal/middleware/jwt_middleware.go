package middleware

import (
	"context"
	"onecloud/internal/pkg/jwt"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// JwtInterceptor gRPC JWT 验证拦截器
func JwtInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (resp interface{}, err error) {
		// 跳过公开接口（登录、注册等）
		if isPublicMethod(info.FullMethod) {
			return handler(ctx, req)
		}

		// 从 metadata 中获取 token
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "缺少认证信息")
		}

		tokenStrings := md.Get("authorization")
		if len(tokenStrings) == 0 {
			return nil, status.Error(codes.Unauthenticated, "缺少 token")
		}

		tokenString := tokenStrings[0]

		// 去掉 "Bearer " 前缀
		if strings.HasPrefix(tokenString, "Bearer ") {
			tokenString = strings.TrimPrefix(tokenString, "Bearer ")
		}

		// 验证 token 并获取 user_id
		newCtx, err := jwt.VerifyToken(tokenString)
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, "token 无效或已过期")
		}

		// 使用新的 context（包含 user_id）调用 handler
		return handler(newCtx, req)
	}
}

// isPublicMethod 判断是否是公开方法（不需要认证）
func isPublicMethod(fullMethod string) bool {
	publicMethods := []string{
		"/storage.User/Register",
		"/storage.User/LoginByAccount",
		"/storage.User/LoginByPhone",
		"/storage.User/LoginBySms",
		"/storage.User/SendVerificationCode",
		"/storage.User/ResetPassword",
		"/storage.User/FindAccount",
	}

	for _, method := range publicMethods {
		if fullMethod == method {
			return true
		}
	}
	return false
}
