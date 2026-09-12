package middleware

import (
	"context"
	"onecloud/internal/pkg/jwt"
	"strings"

	"sync"
	"time"

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
			tokenStrings = md.Get("gateway-authorization")
		}
		if len(tokenStrings) == 0 {
			return nil, status.Error(codes.Unauthenticated, "缺少 token")
		}

		tokenString := tokenStrings[0]

		// 去掉 "Bearer " 前缀
		if strings.HasPrefix(tokenString, "Bearer ") {
			tokenString = strings.TrimPrefix(tokenString, "Bearer ")
		}

		// 验证 token 并获取 user_id
		newCtx, err := jwt.VerifyToken(ctx, tokenString)
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, "token 无效或已过期")
		}

		newCtx, err = injectUserState(newCtx)
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, err.Error())
		}
		// 使用新的 context（包含 user_id）调用 handler
		return handler(newCtx, req)
	}
}

// JwtStreamInterceptor gRPC 流式 JWT 验证拦截器
// server-streaming 方法不经过 UnaryInterceptor，需单独鉴权（与 Unary 同一 token 规则）
func JwtStreamInterceptor() grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		// 流式方法一律要求登录（当前无公开流式方法；新增公开流式方法需在此扩展白名单）
		md, ok := metadata.FromIncomingContext(ss.Context())
		if !ok {
			return status.Error(codes.Unauthenticated, "缺少认证信息")
		}
		tokenStrings := md.Get("authorization")
		if len(tokenStrings) == 0 {
			tokenStrings = md.Get("gateway-authorization")
		}
		if len(tokenStrings) == 0 {
			return status.Error(codes.Unauthenticated, "缺少 token")
		}
		tokenString := tokenStrings[0]
		if strings.HasPrefix(tokenString, "Bearer ") {
			tokenString = strings.TrimPrefix(tokenString, "Bearer ")
		}
		newCtx, err := jwt.VerifyToken(ss.Context(), tokenString)
		if err != nil {
			return status.Error(codes.Unauthenticated, "token 无效或已过期")
		}
		newCtx, err = injectUserState(newCtx)
		if err != nil {
			return status.Error(codes.Unauthenticated, "账号状态异常")
		}
		wrapped := &authenticatedStream{ServerStream: ss, ctx: newCtx}
		return handler(srv, wrapped)
	}
}

// authenticatedStream 用注入 user_id 的 context 包装原始流
type authenticatedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (w *authenticatedStream) Context() context.Context { return w.ctx }

// ===== 用户状态注入（禁用拒绝 + 角色上下文，60s 缓存）=====

type userState struct {
	role     string
	disabled bool
}

var (
	usMu    sync.Mutex
	usCache = map[string]struct {
		state userState
		exp   time.Time
	}{}
)

const userStateTTL = 60 * time.Second

// userStateLookup 由外部注入（避免 middleware → repository 依赖）：userID → role/disabled
var userStateLookup func(ctx context.Context, userID string) (role string, disabled bool, err error)

// SetUserStateLookup 注册用户状态查询函数（main.go 启动时调用）
func SetUserStateLookup(fn func(ctx context.Context, userID string) (string, bool, error)) {
	userStateLookup = fn
}

// injectUserState 校验禁用状态并将角色注入 context
func injectUserState(ctx context.Context) (context.Context, error) {
	userID, err := jwt.GetUserIdFromCtx(ctx)
	if err != nil || userID == "" || userStateLookup == nil {
		return ctx, nil // 无状态查询能力时保持原样（如无 token 的公开流）
	}
	now := time.Now()
	usMu.Lock()
	cached, ok := usCache[userID]
	valid := ok && now.Before(cached.exp)
	usMu.Unlock()
	if !valid {
		role, disabled, err := userStateLookup(ctx, userID)
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, "账号状态查询失败")
		}
		cached = struct {
			state userState
			exp   time.Time
		}{userState{role: role, disabled: disabled}, now.Add(userStateTTL)}
		usMu.Lock()
		usCache[userID] = cached
		usMu.Unlock()
	}
	if cached.state.disabled {
		return nil, status.Error(codes.Unauthenticated, "账号已被禁用")
	}
	return jwt.NewRoleContext(ctx, cached.state.role), nil
}

// isPublicMethod 判断是否是公开方法（不需要认证）
// 注意：方法名必须与 service.proto 中定义的 rpc 方法名完全一致
func isPublicMethod(fullMethod string) bool {
	publicMethods := []string{
		// 用户认证相关
		"/storage.User/Register",
		"/storage.User/LoginByAccount",
		"/storage.User/LoginByPhoneSms",
		"/storage.User/LoginByEmailSms",
		"/storage.User/SendVerificationCode",
		"/storage.User/ResetPassword",
		"/storage.User/FindAccount",
		// 文件分享访问（匿名可访问）
		"/storage.File/AccessShare",
		// 分享下载（匿名可访问）
		"/storage.File/DownloadByShare",
	}

	for _, method := range publicMethods {
		if fullMethod == method {
			return true
		}
	}
	return false
}
