package userlogic

import (
	"context"
	"errors"
	"onecloud/internal/pkg/response"
	"onecloud/internal/repository"

	"onecloud/internal/svc"
	"onecloud/proto/.proto/service"

	"github.com/zeromicro/go-zero/core/logx"
)

type ResetPasswordLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewResetPasswordLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ResetPasswordLogic {
	return &ResetPasswordLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *ResetPasswordLogic) validate(in *service.ResetPasswordReq) error {
	if err := in.Validate(); err != nil {
		var vErr service.ResetPasswordReqValidationError
		if errors.As(err, &vErr) {
			switch vErr.Field() {
			case "Phone":
				return errors.New("手机号码格式错误")
			case "Email":
				return errors.New("邮箱格式错误")
			case "VerificationCode":
				return errors.New("验证码长度错误")
			case "NewPassword":
				return errors.New("密码 6 至 20 位")
			}
		}
		return errors.New("参数校验失败")
	}
	return nil
}

// 重置密码
func (l *ResetPasswordLogic) ResetPassword(in *service.ResetPasswordReq) (*service.Response, error) {
	//校验
	if err := l.validate(in); err != nil {
		return response.Err(err)
	}

	// oneof 守卫：取实际设置的联系字段（原 || 写法在 oneof 下恒为真，接口永久失败）
	contact := in.GetPhone()
	if contact == "" {
		contact = in.GetEmail()
	}
	if contact == "" {
		return response.Err(errors.New("请输入手机号或邮箱"))
	}

	// 校验验证码（对应重置密码类型）
	if err := l.svcCtx.Verifier.Verify(l.ctx, contact, int(service.SmsType_SMS_TYPE_RESET_PASSWORD), in.GetVerificationCode()); err != nil {
		return mapVerifyErr(err)
	}

	userRepository := repository.NewUserRepository(l.svcCtx.EntClient)
	//验证成功 更新密码
	err := userRepository.ResetPassword(l.ctx, in)
	if err != nil {
		return response.Err(err)
	}

	return response.Success(&service.ResetPasswordRes{Success: true})
}
