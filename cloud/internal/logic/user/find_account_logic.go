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
		var vErr service.LoginByAccountReqValidationError
		if errors.As(err, &vErr) {
			switch vErr.Field() {
			case "contact":
				return errors.New("contact")
			case "phone":
				return errors.New("手机号码格式错误")
			case "email":
				return errors.New("邮箱格式错误")
			case "verification_code":
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

	if in.GetPhone() == "" || in.GetEmail() == "" {
		return response.Err(errors.New("请输入手机号或邮箱")) //密码已经在validate校验过
	}

	//对比redis中的验证码
	//只检查对应类型的验证码

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
