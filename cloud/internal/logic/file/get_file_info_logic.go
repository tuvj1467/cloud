package filelogic

import (
	"context"
	"errors"
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

func (l *GetFileInfoLogic) validate(in *service.FileInfoRequest) error {
	if err := in.Validate(); err != nil {
		var vErr service.UploadFileRequestValidationError
		if errors.As(err, &vErr) {
			switch vErr.Field() {
			case "file_id":
				return errors.New("文件id缺失")
			}
		}
		return errors.New("其他校验错误")
	}
	return nil
}

// 获取文件信息
func (l *GetFileInfoLogic) GetFileInfo(in *service.FileInfoRequest) (*service.Response, error) {
	//校验
	if err := l.validate(in); err != nil {
		return response.Err(err)
	}
	fileRepo := repository.NewFileRepository(l.svcCtx.EntClient)
	res, err := fileRepo.GetInfo(l.ctx, in.GetFileId())
	if err != nil {
		return response.Err(err)
	}

	return response.Success(&service.UploadFileResponse{
		File: &service.FileMeta{
			FileId: res.ID, Name: res.Name, Path: res.Path, Suffix: res.Ext, Size: res.Size,
			CreatedAt: res.CreatedAt.String(), UpdatedAt: res.UpdatedAt.String()}})
}
