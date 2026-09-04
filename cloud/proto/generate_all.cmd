

protoc --go_out=.  --validate_out="lang=go:./.proto/service"  --validate_opt=paths=source_relative  base.proto file.proto user.proto bucket.proto


goctl rpc protoc service.proto --go_out=. --go-grpc_out=. --zrpc_out=.. -m  --style=go_zero


protoc --descriptor_set_out=D:/ONE_CLOUD/gateway/protoset.pb --include_imports base.proto file.proto user.proto bucket.proto service.proto
