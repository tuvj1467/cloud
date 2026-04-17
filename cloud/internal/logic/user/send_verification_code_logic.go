package userlogic

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"onecloud/internal/pkg/response"
	"time"

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

// 发送验证码
func (l *SendVerificationCodeLogic) SendVerificationCode(in *service.SendVerificationCodeReq) (*service.Response, error) {
	// 1. 生成 6 位随机验证码
	code := fmt.Sprintf("%06d", rand.New(rand.NewSource(time.Now().UnixNano())).Intn(1000000))

	// 2. 确定联系方式（手机号或邮箱）
	var contact string
	if in.GetPhone() != "" {
		contact = in.GetPhone()
	} else if in.GetEmail() != "" {
		contact = in.GetEmail()
	} else {
		return nil, errors.New("手机号或邮箱不能为空")
	}

	// 3. 存储验证码到 Redis（5 分钟有效期）
	cacheKey := fmt.Sprintf("sms_code:%s:%d", contact, in.Type)
	err := l.svcCtx.Redis.Set(l.ctx, cacheKey, code, 5*time.Minute)
	if err != nil {
		l.Logger.Errorf("存储验证码失败：%v", err)
		return nil, errors.New("验证码发送失败")
	}

	// 4. TODO: 调用短信/邮件服务发送验证码
	// 这里需要接入实际的短信服务商（如阿里云、腾讯云）或邮件服务
	l.Logger.Infof("验证码已生成：%s (联系方式：%s, 类型：%d)", code, contact, in.Type)

	// 开发环境可以返回验证码，生产环境要删除
	// return &service.SendVerificationCodeRes{Success: true, Code: code}, nil

	return response.Success(&service.SendVerificationCodeRes{Success: true})
}
