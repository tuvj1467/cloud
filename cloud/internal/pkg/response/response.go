package response

import (
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

func Err(message error) (*service.Response, error) {
	return &service.Response{
		Code:    500,
		Message: message.Error(),
		Data:    nil,
	}, nil
}
