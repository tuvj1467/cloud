package main

import (
	"context"
	"flag"
	"fmt"
	"onecloud/internal/config"
	"onecloud/internal/middleware"
	"onecloud/internal/pkg/migrate"
	"onecloud/internal/pkg/snowId"
	bucket "onecloud/internal/server/bucket"
	file "onecloud/internal/server/file"
	user "onecloud/internal/server/user"
	"onecloud/internal/svc"
	m_service "onecloud/proto/.proto/service"

	_ "github.com/envoyproxy/protoc-gen-validate/validate"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var configFile = flag.String("f", "etc/storage.yaml", "the config file")

func main() {
	flag.Parse()

	var c config.Config
	//读取配置文件
	conf.MustLoad(*configFile, &c)
	err := snowId.Initialize(0)
	if err != nil {
		panic(fmt.Errorf("雪花 ID 生成器初始化失败: %v", err))
	}
	//注入到svc - 这里会初始化数据库连接
	ctx := svc.NewServiceContext(c)

	// 添加数据库连接状态检查
	if ctx.EntClient == nil {
		panic("数据库连接初始化失败")
	}
	fmt.Println("数据库连接成功!")

	// 存量数据迁移：将 bucket_id 为空的 File 记录回填到 default 桶
	// 仅在 dev 模式自动执行；prod 环境应通过独立命令或脚本执行
	if c.Mode == "dev" {
		if err := migrate.MigrateFileBucket(context.Background(), ctx.EntClient); err != nil {
			fmt.Printf("存量数据迁移失败: %v\n", err)
		}
	}

	// 确保程序退出时关闭所有连接
	defer func() {
		ctx.Close()
		fmt.Println("所有连接已关闭")
	}()

	s := zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		m_service.RegisterFileServer(grpcServer, file.NewFileServer(ctx))
		m_service.RegisterUserServer(grpcServer, user.NewUserServer(ctx))
		m_service.RegisterBucketServer(grpcServer, bucket.NewBucketServer(ctx))

		//if c.Mode == repository.DevMode || c.Mode == repository.TestMode {
		reflection.Register(grpcServer)
		//}

	})
	defer s.Stop()

	// 添加 JWT 拦截器
	s.AddUnaryInterceptors(middleware.JwtInterceptor())

	fmt.Printf("Starting rpc server at %s...\n", c.ListenOn)
	s.Start()
}
