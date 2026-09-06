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
			case "Account":
				return errors.New("用户名/手机号/邮箱必须选择一种")
			case "Name":
				return errors.New("用户名长度必须在 1-20 位")
			case "Phone":
				return errors.New("手机号格式不正确")
			case "Email":
				return errors.New("邮箱格式不正确")
			// 密码
			case "Password":
				return errors.New("密码长度必须在 6-20 位")
			// 验证码
			case "SmsCode":
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

	var id string
	var err error

	switch acc := in.GetAccount().(type) {
	case *service.RegisterReq_Name:
		contact = acc.Name

		// 注册预检：用户名唯一索引冲突前给出业务错误码
		exists, err := userRepository.ExistsByUsername(l.ctx, contact)
		if err != nil {
			return response.Err(errors.New("查询用户失败: " + err.Error()))
		}
		if exists {
			return response.ErrWithCode(response.CodeUserDuplicate, "用户名已被注册")
		}

		//注册账号
		id, err = userRepository.CreateByAccount(l.ctx, l.svcCtx.Snowflake, contact, in.GetPassword())
		if err != nil {
			return response.Err(err)
		}

	case *service.RegisterReq_Phone:
		contact = acc.Phone

		// 手机号注册：验证码必填（proto 注明 == 手动校验）
		if resp, err := l.verifyRegisterCode(contact, in.GetSmsCode()); resp != nil || err != nil {
			return resp, err
		}

		//手机注册账号
		id, err = userRepository.CreateByPhone(l.ctx, l.svcCtx.Snowflake, contact, in.GetName(), in.GetPassword())
		if err != nil {
			return response.Err(err)
		}

	case *service.RegisterReq_Email:
		contact = acc.Email

		// 邮箱注册：验证码必填（proto 注明 == 手动校验）
		if resp, err := l.verifyRegisterCode(contact, in.GetSmsCode()); resp != nil || err != nil {
			return resp, err
		}

		//邮箱注册账号
		id, err = userRepository.CreateByEmail(l.ctx, l.svcCtx.Snowflake, contact, in.GetName(), in.GetPassword())
		if err != nil {
			return response.Err(err)
		}

	}
	res, err := userRepository.GetById(l.ctx, id)
	if err != nil {
		return response.Err(errors.New("查询注册用户失败: " + err.Error()))
	}
	token, err := jwt.GenerateToken(res.ID)
	if err != nil {
		return response.Err(errors.New("生成 token 失败: " + err.Error()))
	}

	return response.Success(&service.RegisterRes{User: &service.UserInfo{
		Id: res.ID, Username: res.Username, Name: res.Nickname, Avatar: &res.Avatar, Phone: &res.Phone, Email: &res.Email,
	}, Token: token})
}

// verifyRegisterCode 手机/邮箱注册的验证码校验：必填 + 组件校验（SMS_TYPE_REGISTER）
func (l *RegisterLogic) verifyRegisterCode(contact, smsCode string) (*service.Response, error) {
	if len(smsCode) != 6 {
		return response.Err(errors.New("请输入 6 位验证码"))
	}
	if err := l.svcCtx.Verifier.Verify(l.ctx, contact, int(service.SmsType_SMS_TYPE_REGISTER), smsCode); err != nil {
		return mapVerifyErr(err)
	}
	return nil, nil
}
