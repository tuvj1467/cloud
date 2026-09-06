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

type FindAccountLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewFindAccountLogic(ctx context.Context, svcCtx *svc.ServiceContext) *FindAccountLogic {
	return &FindAccountLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *FindAccountLogic) validate(in *service.FindAccountReq) error {
	if err := in.Validate(); err != nil {
		var vErr service.FindAccountReqValidationError
		if errors.As(err, &vErr) {
			switch vErr.Field() {
			case "Phone":
				return errors.New("手机号码格式错误")
			case "Email":
				return errors.New("邮箱格式错误")
			case "VerificationCode":
				return errors.New("验证码长度错误")
			}
		}
		return errors.New("参数校验失败")
	}
	return nil
}

// 账号找回
func (l *FindAccountLogic) FindAccount(in *service.FindAccountReq) (*service.Response, error) {
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

	// 校验验证码（找回账号与重置密码同用类型 3 的验证码）
	if err := l.svcCtx.Verifier.Verify(l.ctx, contact, int(service.SmsType_SMS_TYPE_RESET_PASSWORD), in.GetVerificationCode()); err != nil {
		return mapVerifyErr(err)
	}

	userRepository := repository.NewUserRepository(l.svcCtx.EntClient)
	//验证成功 获取对应数据
	res, err := userRepository.FindBack(l.ctx, in)
	if err != nil {
		return response.Err(err)
	}

	return response.Success(&service.FindAccountRes{User: &service.UserInfo{
		Id: res.ID, Username: res.Username, Name: res.Nickname, Avatar: &res.Avatar, Phone: &res.Phone, Email: &res.Email,
	}})
}
