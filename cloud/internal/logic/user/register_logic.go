package userlogic

import (
	"context"
	"errors"
	"fmt"
	"onecloud/internal/pkg/jwt"

	"onecloud/internal/pkg/response"
	"onecloud/internal/repository"
	"onecloud/internal/svc"
	"onecloud/proto/.proto/service"

	"github.com/zeromicro/go-zero/core/logx"
)

type RegisterLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewRegisterLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RegisterLogic {
	return &RegisterLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *RegisterLogic) validate(in *service.RegisterReq) error {
	// 1. 基础Protobuf校验（格式、长度、正则、必填）
	if err := in.Validate(); err != nil {
		var vErr service.RegisterReqValidationError
		if errors.As(err, &vErr) {
			// 精准匹配字段错误
			switch vErr.Field() {
			// oneof账号相关
			case "account":
				return errors.New("用户名/手机号/邮箱必须选择一种")
			case "name":
				return errors.New("用户名长度必须在 1-20 位")
			case "phone":
				return errors.New("手机号格式不正确")
			case "email":
				return errors.New("邮箱格式不正确")
			// 密码
			case "password":
				return errors.New("密码长度必须在 6-20 位")
			// 验证码
			case "sms_code":
				return errors.New("验证码必须为6位数字")
			}
		}
		return errors.New("参数校验失败")
	}

	// 2. 业务逻辑校验（验证码联动：用户名注册不能传验证码）
	switch in.GetAccount().(type) {
	case *service.RegisterReq_Name:
		// 用户名注册 → 不允许传验证码
		if in.SmsCode != nil {
			return errors.New("用户名注册无需填写验证码")
		}
	case *service.RegisterReq_Phone, *service.RegisterReq_Email:
		// 手机号/邮箱注册 → 验证码已通过proto校验，无需重复判断
	}

	return nil
}

// 注册
func (l *RegisterLogic) Register(in *service.RegisterReq) (*service.Response, error) {
	//校验
	if err := l.validate(in); err != nil {
		return response.Err(err)
	}
	userRepository := repository.NewUserRepository(l.svcCtx.EntClient)

	var contact string
	var needVerifyCode bool

	var id string
	var err error

	switch acc := in.GetAccount().(type) {
	case *service.RegisterReq_Name:
		contact = acc.Name
		needVerifyCode = false

		//注册账号
		id, err = userRepository.CreateByAccount(l.ctx, l.svcCtx.Snowflake, contact, in.GetPassword())
		if err != nil {
			return response.Err(err)
		}

	case *service.RegisterReq_Phone:
		contact = acc.Phone
		needVerifyCode = true

		//如果是驗證碼註冊 检验验证码
		if needVerifyCode && in.SmsCode != nil {
			if err := l.verifyCode(contact, *in.SmsCode); err != nil {
				return response.Err(err)
			}
		}

		//手机注册账号
		id, err = userRepository.CreateByPhone(l.ctx, l.svcCtx.Snowflake, contact, in.GetName(), in.GetPassword())
		if err != nil {
			return response.Err(err)
		}

	case *service.RegisterReq_Email:
		contact = acc.Email
		needVerifyCode = true

		//如果是驗證碼註冊 检验验证码
		if needVerifyCode && in.SmsCode != nil {
			if err := l.verifyCode(contact, *in.SmsCode); err != nil {
				return response.Err(err)
			}
		}

		//邮箱注册账号
		id, err = userRepository.CreateByEmail(l.ctx, l.svcCtx.Snowflake, contact, in.GetName(), in.GetPassword())
		if err != nil {
			return response.Err(err)
		}

	}
	res := userRepository.GetById(l.ctx, id)
	token, _ := jwt.GenerateToken(res.ID)

	return response.Success(&service.RegisterRes{User: &service.UserInfo{
		Id: res.ID, Name: res.Nickname, Avatar: &res.Avatar, Phone: &res.Phone, Email: &res.Email,
	}, Token: token})
}

func (l *RegisterLogic) verifyCode(contact, code string) error {
	cacheKey := fmt.Sprintf("sms_code:%s:1", contact)
	storedCode := l.svcCtx.Redis.Get(l.ctx, cacheKey)
	if storedCode == nil {
		return errors.New("验证码不存在或已过期")
	}

	if storedCode.String() != code {
		return errors.New("验证码错误")
	}

	l.svcCtx.Redis.Del(l.ctx, cacheKey)

	return nil
}
