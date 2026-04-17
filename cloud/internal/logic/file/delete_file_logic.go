package filelogic

import (
	"context"

	"onecloud/internal/svc"
	"onecloud/proto/.proto/service"

	"github.com/zeromicro/go-zero/core/logx"
)

type DeleteFileLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewDeleteFileLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteFileLogic {
	return &DeleteFileLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 删除文件
func (l *DeleteFileLogic) DeleteFile(in *service.DeleteFileRequest) (*service.Response, error) {
	// todo: add your logic here and delete this line

	return &service.Response{}, nil
}
