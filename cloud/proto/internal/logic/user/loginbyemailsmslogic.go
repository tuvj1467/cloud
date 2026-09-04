package userlogic

import (
	"context"

	"onecloud/proto/.proto/service"
	"onecloud/proto/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

type LoginByEmailSmsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewLoginByEmailSmsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *LoginByEmailSmsLogic {
	return &LoginByEmailSmsLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *LoginByEmailSmsLogic) LoginByEmailSms(in *service.LoginByEmailSmsReq) (*service.Response, error) {
	// todo: add your logic here and delete this line

	return &service.Response{}, nil
}
