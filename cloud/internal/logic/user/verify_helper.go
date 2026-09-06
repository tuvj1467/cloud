package userlogic

import (
	"errors"

	"onecloud/internal/pkg/response"
	"onecloud/internal/pkg/verification"
	"onecloud/proto/.proto/service"
)

// mapVerifyErr 将验证码组件错误映射为统一响应（携带规范错误码）
func mapVerifyErr(err error) (*service.Response, error) {
	switch {
	case errors.Is(err, verification.ErrCodeExpired):
		return response.ErrWithCode(response.CodeUserSmsCodeExpired, err.Error())
	case errors.Is(err, verification.ErrCodeWrong):
		return response.ErrWithCode(response.CodeUserSmsCodeWrong, err.Error())
	case errors.Is(err, verification.ErrUnavailable):
		return response.ErrWithCode(response.CodeUserSmsCodeService, "验证码服务不可用")
	default:
		return response.Err(err)
	}
}
