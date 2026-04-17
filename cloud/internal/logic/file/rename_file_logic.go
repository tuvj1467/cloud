package filelogic

import (
	"context"

	"onecloud/internal/svc"
	"onecloud/proto/.proto/service"

	"github.com/zeromicro/go-zero/core/logx"
)

type RenameFileLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewRenameFileLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RenameFileLogic {
	return &RenameFileLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 重命名文件
func (l *RenameFileLogic) RenameFile(in *service.RenameFileRequest) (*service.Response, error) {
	// todo: add your logic here and delete this line

	return &service.Response{}, nil
}
