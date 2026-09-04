package filelogic

import (
	"context"
	"errors"

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

// GetFileList 获取当前用户所有文件列表
func (l *GetFileListLogic) GetFileList(in *service.FileListRequest) (*service.Response, error) {
	if err := in.Validate(); err != nil {
		return response.Err(errors.New("参数校验失败"))
	}

	userID, err := jwt.GetUserIdFromCtx(l.ctx)
	if err != nil {
		return response.Err(err)
	}

	fileRepo := repository.NewFileRepository(l.svcCtx.EntClient)
	list, total, err := fileRepo.GetPageFileList(l.ctx, userID, in)
	if err != nil {
		return response.Err(errors.New("查询文件列表失败: " + err.Error()))
	}

	files := make([]*service.FileMeta, 0, len(list))
	for _, f := range list {
		files = append(files, toFileMeta(f))
	}

	return response.Success(&service.FileListResponse{
		Files: files,
		Total: int32(total),
	})
}
