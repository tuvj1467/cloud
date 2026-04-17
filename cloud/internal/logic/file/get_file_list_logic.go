package filelogic

import (
	"context"
	"onecloud/internal/pkg/jwt"
	"onecloud/internal/pkg/response"
	"onecloud/internal/repository"
	"onecloud/internal/svc"
	"onecloud/proto/.proto/service"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetFileListLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetFileListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetFileListLogic {
	return &GetFileListLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 获取文件列表
func (l *GetFileListLogic) GetFileList(in *service.FileListRequest) (*service.Response, error) {
	userId, err := jwt.GetUserIdFromCtx(l.ctx)
	if err != nil {
		return response.Err(err)
	}

	fileRepo := repository.NewFileRepository(l.svcCtx.EntClient)
	res, err := fileRepo.GetFileList(l.ctx, userId)

	return &service.Response{}, nil
}
