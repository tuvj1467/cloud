package filelogic

import (
	"context"
	"errors"

	"onecloud/ent"
	"onecloud/internal/pkg/jwt"
	"onecloud/internal/pkg/response"
	"onecloud/internal/repository"
	"onecloud/internal/svc"
	"onecloud/proto/.proto/service"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetFileInfoLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetFileInfoLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetFileInfoLogic {
	return &GetFileInfoLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// GetFileInfo 获取文件信息
func (l *GetFileInfoLogic) GetFileInfo(in *service.FileInfoRequest) (*service.Response, error) {
	if err := in.Validate(); err != nil {
		return response.Err(errors.New("参数校验失败"))
	}

	userID, err := jwt.GetUserIdFromCtx(l.ctx)
	if err != nil {
		return response.Err(err)
	}

	fileRepo := repository.NewFileRepository(l.svcCtx.EntClient)
	f, err := fileRepo.GetInfo(l.ctx, in.GetFileId())
	if err != nil {
		if ent.IsNotFound(err) {
			return response.Err(errors.New("文件不存在"))
		}
		return response.ErrInternal(l.Logger, "查询文件失败", err)
	}

	// 校验文件归属
	if f.UserID != userID {
		return response.Err(errors.New("无权访问该文件"))
	}
	// 回收站中的文件按不存在处理（列表已过滤，详情不再暴露）
	if f.DeletedAt != nil {
		return response.ErrWithCode(response.CodeFileNotFound, "文件不存在")
	}

	return response.Success(&service.FileInfoResponse{
		File: toFileMeta(f),
	})
}
