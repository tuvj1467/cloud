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
		var vErr service.LoginByAccountReqValidationError
		if errors.As(err, &vErr) {
			switch vErr.Field() {
			case "phone":
				return errors.New("手机号码格式错误")
			case "smscode":
				return errors.New("验证码6位")
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

	if in.GetPhone() == "" || in.GetSmsCode() == "" {
		return response.Err(errors.New("请输入手机号和验证码"))

	}

	//对比redis中的验证码

	//验证成功 通过phone获取ID
	userRepository := repository.NewUserRepository(l.svcCtx.EntClient)
	//通过Phone查找账号 对比密码
	res, err := userRepository.LoginByPhoneSms(l.ctx, in)
	if err != nil {
		return response.Err(err)
	}
	//分发token
	token, _ := jwt.GenerateToken(res.ID)

	return response.Success(&service.LoginRes{User: &service.UserInfo{
		Id: res.ID, Username: res.Username, Name: res.Nickname, Avatar: &res.Avatar, Phone: &res.Phone, Email: &res.Email,
	}, Token: token})
}
