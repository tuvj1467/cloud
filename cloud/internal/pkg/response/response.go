package response

import (
	"fmt"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/anypb"
	"onecloud/proto/.proto/service"
)

// Logger 日志接口（取 logx.Logger 子集，便于单测注入）
type Logger = logx.Logger

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

// ErrInternal 内部错误统一出口：完整错误细节（含 DB/存储层原文、本地路径）
// 只进服务端日志，客户端收到「操作文案 + 失败」通用信息，防止内部拓扑泄漏
// op 为操作描述（如 "查询文件失败"），err 为原始错误
func ErrInternal(logger Logger, op string, err error) (*service.Response, error) {
	if logger != nil {
		logger.Errorf("%s: %v", op, err)
	}
	return &service.Response{
		Code:    CodeSystemInternal,
		Message: op + "，请稍后重试或联系管理员",
		Data:    nil,
	}, nil
}

// ErrInternalWithCode 同 ErrInternal，但允许指定业务错误码
func ErrInternalWithCode(logger Logger, code int32, op string, err error) (*service.Response, error) {
	if logger != nil {
		logger.Errorf("%s: %v", op, err)
	}
	return &service.Response{
		Code:    code,
		Message: op + "，请稍后重试或联系管理员",
		Data:    nil,
	}, nil
}
