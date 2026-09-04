package filelogic

import (
	"context"

	"onecloud/proto/.proto/service"
	"onecloud/proto/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

type CreateShareLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCreateShareLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateShareLogic {
	return &CreateShareLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// ===== 文件分享 =====
func (l *CreateShareLogic) CreateShare(in *service.CreateShareRequest) (*service.Response, error) {
	// todo: add your logic here and delete this line

	return &service.Response{}, nil
}
