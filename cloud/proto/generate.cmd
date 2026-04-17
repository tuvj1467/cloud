#    -go_out 指定 message 生成路径    --go-grpc_out service 生成路径    --zrpc_out=. zrpc生成的骨架(internal

protoc --go_out=.  --validate_out="lang=go:./.proto/service"  --validate_opt=paths=source_relative  base.proto file.proto user.proto


goctl rpc protoc service.proto --go_out=. --go-grpc_out=. --zrpc_out=.. -m


protoc --go_out=.    base.proto file.proto user.proto

protoc --go_out=./.proto/service --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative  base.proto file.proto user.proto



--go-grpc_opt=paths=source_relative

#
protoc --descriptor_set_out=gateway/hello.pb hello.proto

goctl rpc protoc file.proto --go_out=.. --go_opt=paths=source_relative  --go-grpc_out=.. --zrpc_out=..

protoc --proto_path=. --proto_path=. --descriptor_set_out=D:\ONE_CLOUD\gateway\protoset.pb --include_imports validate.proto user.proto file.proto base.proto service.proto