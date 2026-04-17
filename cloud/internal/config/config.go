package config

import (
	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	zrpc.RpcServerConf
	MySQL DB

	Redis struct {
		Addr     string `json:",optional"`
		Password string `json:",optional"`
		DB       int    `json:",optional"`
		PoolSize int    `json:",optional"`
	} `json:",optional"`
}

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
