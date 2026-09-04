package userlogic

import (
	"context"

	"onecloud/proto/.proto/service"
	"onecloud/proto/internal/svc"

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

func (l *LoginByPhoneSmsLogic) LoginByPhoneSms(in *service.LoginByPhoneSmsReq) (*service.Response, error) {
	// todo: add your logic here and delete this line

	return &service.Response{}, nil
}
