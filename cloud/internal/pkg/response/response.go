package response

import (
	"fmt"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/anypb"
	"onecloud/proto/.proto/service"
)

type Response struct {
	code    int32
	message string
	data    *anypb.Any
}

func Success(data protoreflect.ProtoMessage) (*service.Response, error) {
	// 将 interface{} 转换为 *anypb.Any
	var anyData *anypb.Any
	if data != nil {
		var err error
		anyData, err = anypb.New(data)
		if err != nil {
			return &service.Response{
				Code:    500,
				Message: "Failed to marshal data",
				Data:    nil,
			}, err
		}
	}

	return &service.Response{
		Code:    200,
		Message: "Success",
		Data:    anyData,
	}, nil
}

// Err 通用错误响应（兜底使用 CodeSystemInternal）
// 推荐关键路径使用 ErrWithCode 携带规范错误码
func Err(message error) (*service.Response, error) {
	return &service.Response{
		Code:    CodeSystemInternal,
		Message: message.Error(),
		Data:    nil,
	}, nil
}

// ErrWithCode 携带错误码的错误响应
// 用于客户端按 code 精确识别错误类型
func ErrWithCode(code int32, msg string) (*service.Response, error) {
	return &service.Response{
		Code:    code,
		Message: msg,
		Data:    nil,
	}, nil
}

// ErrWithCodef 格式化的带码错误响应
func ErrWithCodef(code int32, format string, args ...interface{}) (*service.Response, error) {
	return &service.Response{
		Code:    code,
		Message: fmt.Sprintf(format, args...),
		Data:    nil,
	}, nil
}
