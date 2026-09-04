package userlogic

import (
	"context"

	"onecloud/proto/.proto/service"
	"onecloud/proto/internal/svc"

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

// 三种登录方式
func (l *LoginByAccountLogic) LoginByAccount(in *service.LoginByAccountReq) (*service.Response, error) {
	// todo: add your logic here and delete this line

	return &service.Response{}, nil
}
