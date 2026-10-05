package userlogic

import (
	"context"
	"errors"
	"onecloud/internal/pkg/jwt"
	"onecloud/internal/pkg/response"
	"onecloud/internal/repository"

	"onecloud/internal/svc"
	"onecloud/proto/.proto/service"

	"github.com/zeromicro/go-zero/core/logx"
)

type LoginByPhoneSmsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewLoginByPhoneSmsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *LoginByPhoneSmsLogic {
	return &LoginByPhoneSmsLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *LoginByPhoneSmsLogic) validate(in *service.LoginByPhoneSmsReq) error {
	if err := in.Validate(); err != nil {
		var vErr service.LoginByPhoneSmsReqValidationError
		if errors.As(err, &vErr) {
			switch vErr.Field() {
			case "Phone":
				return errors.New("手机号格式不正确")
			case "SmsCode":
				return errors.New("验证码必须为 6 位")
			}
		}
		return errors.New("参数校验失败")
	}
	return nil
}

func (l *LoginByPhoneSmsLogic) LoginByPhoneSms(in *service.LoginByPhoneSmsReq) (*service.Response, error) {
	//校验
	if err := l.validate(in); err != nil {
		return response.Err(err)
	}

	// 校验验证码（组件内含一次性消费与失败计数）
	if err := l.svcCtx.Verifier.Verify(l.ctx, in.GetPhone(), int(service.SmsType_SMS_TYPE_LOGIN), in.GetSmsCode()); err != nil {
		return mapVerifyErr(err)
	}

	//验证成功 通过phone获取ID
	userRepository := repository.NewUserRepository(l.svcCtx.EntClient)
	//通过Phone查找账号 对比密码
	res, err := userRepository.LoginByPhoneSms(l.ctx, in)
	if err != nil {
		return response.Err(err)
	}

	// 禁用账号拒绝登录
	if res.Disabled {
		return response.ErrWithCode(response.CodeUserDisabled, "账号已被禁用")
	}
	token, err := jwt.GenerateToken(res.ID, res.TokenVersion)
	if err != nil {
		return response.ErrInternal(l.Logger, "生成 token 失败", err)
	}

	return response.Success(&service.LoginRes{User: &service.UserInfo{
		Id: res.ID, Username: res.Username, Name: res.Nickname, Avatar: &res.Avatar, Phone: &res.Phone, Email: &res.Email,
	}, Token: token})
}
