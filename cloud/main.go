package main

import (
	"context"
	"flag"
	"fmt"
	"onecloud/ent"
	"onecloud/internal/config"
	"onecloud/internal/middleware"
	"onecloud/internal/pkg/cleanup"
	"onecloud/internal/pkg/loginlock"
	"onecloud/internal/pkg/migrate"
	"onecloud/internal/pkg/snapshot"
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

		// gRPC reflection 仅 dev 模式开启（生产面收敛：不对外暴露服务描述）
		if c.Mode == "dev" {
			reflection.Register(grpcServer)
		}

	})
	defer s.Stop()

	// 登录失败锁定（Redis 计数，Redis 不可用时进程内回退）
	loginlock.Setup(ctx.Redis)

	// 用户状态查询注入（JWT 拦截器禁用检查与角色注入的数据源）
	middleware.SetUserStateLookup(func(rctx context.Context, userID string) (string, bool, int64, error) {
		u, err := ctx.EntClient.User.Get(rctx, userID)
		if err != nil {
			if ent.IsNotFound(err) {
				return "user", false, 0, nil // 用户已删除：按普通用户放行（文件归属校验兜底）
			}
			return "", false, 0, err
		}
		return string(u.Role), u.Disabled, u.TokenVersion, nil
	})

	// 用户级限流（须在 JWT 之前：匿名也按 IP 限流）
	middleware.SetupRateLimit(c.RateLimit.Enabled, c.RateLimit.QPS, c.RateLimit.Burst, c.RateLimit.SendCodeEvery)
	if c.RateLimit.Enabled {
		s.AddUnaryInterceptors(middleware.RateLimitUnaryInterceptor())
	}

	// 添加 JWT 拦截器（Unary + Stream：流式方法如 DownloadStream 需单独鉴权）
	s.AddUnaryInterceptors(middleware.JwtInterceptor())
	s.AddStreamInterceptors(middleware.JwtStreamInterceptor())

	// 分片上传遗留清理：孤儿 .tmp 目录 + 过期 pending 分片记录（启动 30s 后首扫，每 12h 一次）
	cleanupCtx, cleanupCancel := context.WithCancel(context.Background())
	defer cleanupCancel()
	cleanup.Start(cleanupCtx, ctx.EntClient, c.Storage.Local.RootDir, ctx.Storage, ctx.Redis)

	// 用量快照：启动 20s 后历史补齐 + 每日 00:05 聚合（趋势报表数据源）
	snapshot.Start(cleanupCtx, ctx.EntClient, ctx.Snowflake)

	// 放宽 gRPC 单消息接收上限至 110MB：上传/分片（含 JSON base64 膨胀）与大文件下载
	// 需要超过默认 4MB；与网关 MaxBytes、业务 MaxUploadSize（100MB）对齐
	s.AddOptions(grpc.MaxRecvMsgSize(110 * 1024 * 1024))
	s.AddOptions(grpc.MaxSendMsgSize(110 * 1024 * 1024))

	fmt.Printf("Starting rpc server at %s...\n", c.ListenOn)
	s.Start()
}
