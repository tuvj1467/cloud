package main

import (
	"flag"
	"fmt"
	"net/http"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/gateway"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
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

	// go-zero gateway 的 ProcessHeaders 仅转发 Grpc-Metadata-* 前缀 header（映射为 gateway-* metadata），
	// 浏览器 fetch 无法设置该前缀（fetch 规范禁止以 Grpc- 开头的 header），
	// 这里把标准 Authorization header 桥接为 gateway-authorization，与 cloud jwt_middleware 的 fallback 匹配，
	// 使前端 Authorization: Bearer <token> 正常鉴权；Grpc-Metadata-Authorization 透传路径不受影响。
	server := gateway.MustNewServer(c.GatewayConf,
		gateway.WithHeaderProcessor(func(header http.Header) []string {
			var vals []string
			for _, v := range header.Values("Authorization") {
				vals = append(vals, "gateway-authorization:"+v) // 原样转发，Bearer 前缀由 jwt_middleware 统一剥离
			}
			// 审计用：转发客户端 IP 与 UA（share 访问/下载日志记录来源）
			if xff := header.Values("X-Forwarded-For"); len(xff) > 0 {
				vals = append(vals, "gateway-x-forwarded-for:"+xff[0])
			}
			if ua := header.Values("User-Agent"); len(ua) > 0 {
				vals = append(vals, "gateway-user-agent:"+ua[0])
			}
			return vals
		}),
		// 放宽网关内部 gRPC 客户端的消息上限（go-zero 默认接收 4MB，导致 >4MB 下载 429），
		// 与 cloud 侧 MaxRecv/SendMsgSize 及 MaxUploadSize（100MB）对齐
		gateway.WithDialer(func(conf zrpc.RpcClientConf) zrpc.Client {
			return zrpc.MustNewClient(conf, zrpc.WithDialOption(
				grpc.WithDefaultCallOptions(
					grpc.MaxCallRecvMsgSize(110*1024*1024),
					grpc.MaxCallSendMsgSize(110*1024*1024),
				),
			))
		}),
	)

	fmt.Printf("Gateway started at %s:%d\n", c.Host, c.Port)
	server.Start()
}
