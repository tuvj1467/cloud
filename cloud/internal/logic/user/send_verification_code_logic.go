package userlogic

import (
	"context"
	"errors"

	"onecloud/internal/pkg/response"
	"onecloud/internal/pkg/verification"
	"onecloud/internal/svc"
	"onecloud/proto/.proto/service"

	"github.com/zeromicro/go-zero/core/logx"
)

type SendVerificationCodeLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewSendVerificationCodeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SendVerificationCodeLogic {
	return &SendVerificationCodeLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// SendVerificationCode 发送验证码（组件内含 60 秒频率限制与 5 分钟有效期）
func (l *SendVerificationCodeLogic) SendVerificationCode(in *service.SendVerificationCodeReq) (*service.Response, error) {
	// 1. 确定联系方式（oneof：手机号或邮箱二选一）
	contact := in.GetPhone()
	if contact == "" {
		contact = in.GetEmail()
	}
	if contact == "" {
		return response.Err(errors.New("手机号或邮箱不能为空"))
	}

	// 2. 校验验证码类型（0 为未知类型，拒绝发送）
	if in.Type == service.SmsType_SMS_TYPE_UNKNOWN {
		return response.Err(errors.New("无效的验证码类型"))
	}

	// 3. 生成 + 存储 + 发送
	if err := l.svcCtx.Verifier.Send(l.ctx, contact, int(in.Type)); err != nil {
		switch {
		case errors.Is(err, verification.ErrTooFrequent):
			return response.ErrWithCode(response.CodeUserSmsCodeFrequent, err.Error())
		case errors.Is(err, verification.ErrUnavailable):
			return response.ErrWithCode(response.CodeUserSmsCodeService, "验证码服务不可用")
		default:
			l.Logger.Errorf("发送验证码失败 contact=%s type=%d: %v", contact, in.Type, err)
			return response.ErrWithCode(response.CodeUserSmsCodeService, "验证码发送失败")
		}
	}

	return response.Success(&service.SendVerificationCodeRes{Success: true})
}
