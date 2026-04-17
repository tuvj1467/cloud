package filelogic

import (
	"context"

	"onecloud/internal/svc"
	"onecloud/proto/.proto/service"

	"github.com/zeromicro/go-zero/core/logx"
)

type DownloadFileLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewDownloadFileLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DownloadFileLogic {
	return &DownloadFileLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 下载文件
func (l *DownloadFileLogic) DownloadFile(in *service.DownloadFileRequest) (*service.Response, error) {
	// todo: add your logic here and delete this line

	return &service.Response{}, nil
}
