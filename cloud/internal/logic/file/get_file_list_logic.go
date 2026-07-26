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
	res, count, err := fileRepo.GetPageFileList(l.ctx, userId, in)
	if err != nil {
		return response.Err(err)
	}

	files := make([]*service.FileMeta, 0, len(res))
	for _, item := range res {
		files = append(files, &service.FileMeta{
			FileId:    item.ID,
			Name:      item.Name,
			Path:      item.Path,
			Size:      item.Size,
			Suffix:    item.Ext,
			UserId:    item.UserID,
			CreatedAt: item.CreatedAt.Format("2006-01-02 15:04:05"),
			UpdatedAt: item.UpdatedAt.Format("2006-01-02 15:04:05"),
		})
	}

	return response.Success(&service.FileListResponse{
		Files: files,
		Total: int32(count),
	})
}
