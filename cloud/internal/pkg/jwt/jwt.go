package jwt

import (
	"context"
	"errors"
	"github.com/golang-jwt/jwt/v4"
	"time"
)

// 自定义一个字符串
var jwtkey = []byte("www.topgoer.com")

type Claims struct {
	UserId string `json:"id"`
	jwt.RegisteredClaims
}

// 颁发 token
func GenerateToken(userId string) (string, error) {
	expireTime := time.Now().Add(7 * 24 * time.Hour)
	claims := &Claims{
		UserId: userId,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expireTime), // 过期时间
			IssuedAt:  jwt.NewNumericDate(time.Now()), // 签发时间
			Issuer:    "127.0.0.1",                    // 签名颁发者
			Subject:   "user token",                   // 签名主题
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(jwtkey)
	if err != nil {
		return "", err
	}
	return tokenString, nil
}

// 解析 token
func ParseToken(tokenString string) (*jwt.Token, *Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		return jwtkey, nil
	})
	return token, claims, err
}

// context key 类型
type contextKey string

const userIdKey = contextKey("user_id")

// 将 user_id 注入到 context 中
func NewContext(ctx context.Context, userId string) context.Context {
	return context.WithValue(ctx, userIdKey, userId)
}

// 从 context 中获取 user_id
func GetUserIdFromCtx(ctx context.Context) (string, error) {
	userId, ok := ctx.Value(userIdKey).(string)
	if !ok || userId == "" {
		return "", errors.New("user_id 不存在")
	}
	return userId, nil
}

// JWT 验证中间件（适用于 go-zero 的 rpc/http 中间件）
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
