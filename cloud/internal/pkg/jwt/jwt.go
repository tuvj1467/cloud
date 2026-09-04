package jwt

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

// 默认配置（未调用 Init 时使用，保持向后兼容）
const (
	defaultSecret = "www.topgoer.com"
	defaultExpire = int64(7 * 24 * 3600) // 7 天
	defaultIssuer = "127.0.0.1"
)

// 全局配置（通过 Init 设置，线程安全）
var (
	cfgMu     sync.RWMutex
	cfgSecret = []byte(defaultSecret)
	cfgExpire = defaultExpire
	cfgIssuer = defaultIssuer
)

// Init 初始化 JWT 配置（启动时由 ServiceContext 调用）
func Init(secret string, expire int64, issuer string) {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	if secret != "" {
		cfgSecret = []byte(secret)
	}
	if expire > 0 {
		cfgExpire = expire
	}
	if issuer != "" {
		cfgIssuer = issuer
	}
}

// Claims 自定义 JWT 声明
type Claims struct {
	UserId string `json:"id"`
	jwt.RegisteredClaims
}

// GenerateToken 颁发 token
func GenerateToken(userId string) (string, error) {
	cfgMu.RLock()
	secret := cfgSecret
	expire := cfgExpire
	issuer := cfgIssuer
	cfgMu.RUnlock()

	expireTime := time.Now().Add(time.Duration(expire) * time.Second)
	claims := &Claims{
		UserId: userId,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expireTime), // 过期时间
			IssuedAt:  jwt.NewNumericDate(time.Now()), // 签发时间
			Issuer:    issuer,                         // 签名颁发者
			Subject:   "user token",                   // 签名主题
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(secret)
	if err != nil {
		return "", err
	}
	return tokenString, nil
}

// ParseToken 解析 token
func ParseToken(tokenString string) (*jwt.Token, *Claims, error) {
	cfgMu.RLock()
	secret := cfgSecret
	cfgMu.RUnlock()

	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		return secret, nil
	})
	return token, claims, err
}

// context key 类型
type contextKey string

const userIdKey = contextKey("user_id")

// NewContext 将 user_id 注入到 context 中
func NewContext(ctx context.Context, userId string) context.Context {
	return context.WithValue(ctx, userIdKey, userId)
}

// GetUserIdFromCtx 从 context 中获取 user_id
func GetUserIdFromCtx(ctx context.Context) (string, error) {
	userId, ok := ctx.Value(userIdKey).(string)
	if !ok || userId == "" {
		return "", errors.New("user_id 不存在")
	}
	return userId, nil
}

// VerifyToken 验证 token 并返回包含 user_id 的新 context
func VerifyToken(tokenString string) (context.Context, error) {
	if tokenString == "" {
		return nil, errors.New("token 不能为空")
	}

	token, claims, err := ParseToken(tokenString)
	if err != nil || !token.Valid {
		return nil, errors.New("token 无效")
	}

	// 将 user_id 注入到 context 中
	ctx := NewContext(context.Background(), claims.UserId)
	return ctx, nil
}
