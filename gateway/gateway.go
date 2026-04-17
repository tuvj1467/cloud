package main

import (
	"flag"
	"fmt"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/gateway"
	"log"
)

// 定义配置结构体（和storage-gateway.yaml对应）
type GatewayConfig struct {
	gateway.GatewayConf
}

var configFile = flag.String("f", "./gateway.yaml", "Gateway配置文件路径")

func main() {
	// 解析命令行参数
	flag.Parse()

	var c GatewayConfig
	// 加载Gateway配置
	conf.MustLoad(*configFile, &c)

	// ---------------------- 核心：给 go-zero 网关配置自定义选项 ----------------------
	// 1. 创建自定义序列化器实例
	//customMarshaler := &types.CustomMarshaler{}
	//// 2. 构造 grpc-gateway 的 ServeMux 选项（注入到 go-zero 的 GatewayConf）
	//c.GatewayConf.MuxOptions = []runtime.ServeMuxOption{
	//	// 配置自定义序列化器（全局生效）
	//	runtime.WithMarshalerOption(runtime.MIMEWildcard, customMarshaler),
	//	// 配置自定义错误处理器（全局生效）
	//	runtime.WithErrorHandler(types.CustomErrorHandler),
	//}

	// 关键：打印加载后的配置，确认upstreams和mapping是否正确解析
	log.Printf("=== 加载的网关配置 ===")
	log.Printf("网关地址：%s:%d", c.Host, c.Port)
	log.Printf("上游服务数量：%d", len(c.Upstreams))
	// 遍历打印所有路由规则（核心：确认 mapping 是否加载）
	for i, upstream := range c.Upstreams {
		log.Printf("上游服务%d: %s", i, upstream.Name)
		log.Printf("  gRPC地址：%s", upstream.Grpc.Target)
		log.Printf("  路由规则数量：%d", len(upstream.Mappings))
		for j, mapping := range upstream.Mappings {
			log.Printf("    路由%d: %s %s -> %s", j, mapping.Method, mapping.Path, mapping.RpcPath)
		}
	}

	// 创建Gateway服务器
	server := gateway.MustNewServer(c.GatewayConf)

	// 启动Gateway
	fmt.Printf("Gateway started at %s:%d\n", c.Host, c.Port)
	server.Start()
}
