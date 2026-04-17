package types

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// CustomMarshaler 自定义序列化器，替换默认的 JSON Marshaler
type CustomMarshaler struct {
	runtime.JSONBuiltin // 继承默认的 JSON 序列化逻辑
}

// Marshal 重写序列化方法：把所有响应封装成 types.Response
func (c *CustomMarshaler) Marshal(v interface{}) ([]byte, error) {
	// 构造自定义响应结构
	resp := Response{
		Code:    0,
		Message: "success",
		Data:    v, // 成功时，把原响应数据放到 Data 字段
	}
	return json.Marshal(resp)
}

// 自定义错误处理函数：拦截 gRPC 错误，封装成 types.Response
func CustomErrorHandler(ctx context.Context, mux *runtime.ServeMux, marshaler runtime.Marshaler, w http.ResponseWriter, r *http.Request, err error) {
	// 解析 gRPC 错误状态
	st, ok := status.FromError(err)
	if !ok {
		st = status.New(codes.Internal, "内部服务器错误")
	}

	// 构造自定义错误响应
	customResp := Response{
		Code:    int(st.Code()), // gRPC 状态码转自定义业务码
		Message: st.Message(),   // 错误信息
		Data:    nil,            // 错误时 Data 为 nil
	}

	// 设置 HTTP 响应头
	w.Header().Set("Content-Type", "application/json")
	// 映射 gRPC 状态码到 HTTP 状态码
	w.WriteHeader(runtime.HTTPStatusFromCode(st.Code()))
	// 序列化自定义响应
	json.NewEncoder(w).Encode(customResp)
}
