REM proto 全量再生成脚本（在 cloud/proto 目录执行）
REM 1. 消息代码 + 校验代码
protoc --go_out=.  --validate_out="lang=go:./.proto/service"  --validate_opt=paths=source_relative  base.proto file.proto user.proto bucket.proto

REM 2. gRPC 服务代码（消息 + 服务端/客户端桩）
protoc --go_out=. --go-grpc_out=. service.proto

REM 3. 网关运行时描述符集（相对本仓库根的 gateway 目录）
protoc --descriptor_set_out=../../gateway/protoset.pb --include_imports base.proto file.proto user.proto bucket.proto service.proto
