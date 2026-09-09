package userlogic

import (
	"context"
	"errors"
	"onecloud/ent"
	"onecloud/internal/pkg/jwt"
	"onecloud/internal/pkg/response"
	"onecloud/internal/repository"
	"regexp"

	"onecloud/internal/svc"
	"onecloud/proto/.proto/service"

	"github.com/zeromicro/go-zero/core/logx"
)

type LoginByAccountLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewLoginByAccountLogic(ctx context.Context, svcCtx *svc.ServiceContext) *LoginByAccountLogic {
	return &LoginByAccountLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}
func (l *LoginByAccountLogic) validate(in *service.LoginByAccountReq) error {
	if err := in.Validate(); err != nil {
		var vErr service.LoginByAccountReqValidationError
		if errors.As(err, &vErr) {
			switch vErr.Field() {
			case "Account":
				return errors.New("账号长度至少为 1")
			case "Password":
				return errors.New("密码长度为 6-20 位")
			}
		}
		return errors.New("参数校验失败")
	}
	return nil
}

// 手机号正则（国内标准）
var phoneRegex = regexp.MustCompile(`^1[3-9]\d{9}$`)

// 邮箱正则（通用标准）
var emailRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)

// 三种登录方式
func (l *LoginByAccountLogic) LoginByAccount(in *service.LoginByAccountReq) (*service.Response, error) {
	//校验
	if err := l.validate(in); err != nil {
		return response.Err(err)
	}
	if in.Account == "" || in.Password == "" {
		return response.Err(errors.New("请输入账号和密码"))
	}

	userRepository := repository.NewUserRepository(l.svcCtx.EntClient)

	//判断登录方式
	var query *ent.User
	var err error
	// 1. 自动识别账号类型，构建查询条件
	switch {
	case phoneRegex.MatchString(in.GetAccount()):
		// 手机号登录
		query, err = userRepository.LoginByPhone(l.ctx, in)
		if err != nil {
			return response.Err(err)
		}

	case emailRegex.MatchString(in.GetAccount()):
		// 邮箱登录
		query, err = userRepository.LoginByEmail(l.ctx, in)
		if err != nil {
			return response.Err(err)
		}

	default:
		// 用户名登录
		query, err = userRepository.LoginByName(l.ctx, in)
		if err != nil {
			return response.Err(err)
		}
	}

	//分发token
	token, err := jwt.GenerateToken(query.ID)
	if err != nil {
		return response.Err(errors.New("生成 token 失败: " + err.Error()))
	}

	return response.Success(&service.LoginRes{User: &service.UserInfo{
		Id: query.ID, Username: query.Username, Name: query.Nickname, Avatar: &query.Avatar, Phone: &query.Phone, Email: &query.Email,
	}, Token: token})
}
