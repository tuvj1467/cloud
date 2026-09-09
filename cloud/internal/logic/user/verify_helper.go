package userlogic

import (
	"errors"

	"github.com/zeromicro/go-zero/core/logx"
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
		// 未知错误不透出内部细节（可能含 KV 后端信息），只记日志回通用文案
		logx.Errorf("验证码组件未知错误: %v", err)
		return response.ErrWithCode(response.CodeSystemInternal, "验证码服务异常，请稍后重试")
	}
}
