package main

import (
	"flag"
	"fmt"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/gateway"
)

type GatewayConfig struct {
	gateway.GatewayConf
}

var configFile = flag.String("f", "./gateway.yaml", "Gateway配置文件路径")

func main() {
	flag.Parse()

	var c GatewayConfig
	conf.MustLoad(*configFile, &c)

	fmt.Printf("=== 加载的网关配置 ===\n")
	fmt.Printf("网关地址：%s:%d\n", c.Host, c.Port)
	fmt.Printf("上游服务数量：%d\n", len(c.Upstreams))
	for i, upstream := range c.Upstreams {
		fmt.Printf("上游服务%d: %s\n", i, upstream.Name)
		fmt.Printf("  gRPC地址：%s\n", upstream.Grpc.Target)
		fmt.Printf("  路由规则数量：%d\n", len(upstream.Mappings))
		for j, mapping := range upstream.Mappings {
			fmt.Printf("    路由%d: %s %s -> %s\n", j, mapping.Method, mapping.Path, mapping.RpcPath)
		}
	}

	server := gateway.MustNewServer(c.GatewayConf)

	fmt.Printf("Gateway started at %s:%d\n", c.Host, c.Port)
	server.Start()
}
