package userlogic

import (
	"context"

	"onecloud/proto/.proto/service"
	"onecloud/proto/internal/svc"

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

// 账号找回
func (l *FindAccountLogic) FindAccount(in *service.FindAccountReq) (*service.Response, error) {
	// todo: add your logic here and delete this line

	return &service.Response{}, nil
}
