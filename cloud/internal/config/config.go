package config

import (
	"onecloud/internal/pkg/storage"

	"github.com/zeromicro/go-zero/zrpc"
)

// Config 服务配置（对应 etc/storage.yaml）
type Config struct {
	zrpc.RpcServerConf
	MySQL     DB
	RedisConf RedisConf
	Storage   storage.Config // 存储驱动配置
	JWT       JWTConf        // JWT 鉴权配置
	RateLimit RateLimitConf  `json:",optional"` // 用户级 API 限流（缺省关闭）
}

// RateLimitConf 用户级 API 限流配置
type RateLimitConf struct {
	Enabled       bool    `json:",optional"`   // 是否启用（缺省关闭）
	QPS           float64 `json:",default=10"` // 每身份每秒请求数
	Burst         int     `json:",default=20"` // 令牌桶突发容量
	SendCodeEvery int     `json:",default=20"` // 发送验证码最小间隔（秒），约 3/min
}

// DB MySQL 数据库配置
type DB struct {
	Host            string `json:",optional"`
	User            string `json:",optional"`
	Password        string `json:",optional"`
	Database        string `json:",optional"`
	Charset         string `json:",optional"`
	MaxOpenConns    int    `json:",optional"`
	MaxIdleConns    int    `json:",optional"`
	ConnMaxLifetime int    `json:",optional"`
	ConnMaxIdleTime int    `json:",optional"`
	Debug           bool   `json:",optional"`
	Loc             string `json:",optional"`
}

// RedisConf Redis 缓存配置
type RedisConf struct {
	Host     string `json:",optional"`
	Password string `json:",optional"`
	DB       int    `json:",optional"`
	PoolSize int    `json:",optional"`
}

// JWTConf JWT 鉴权配置
type JWTConf struct {
	Secret string `json:",optional"`          // 签名密钥
	Expire int64  `json:",default=604800"`    // 过期时间（秒），默认 7 天
	Issuer string `json:",default=127.0.0.1"` // 签发者
}
