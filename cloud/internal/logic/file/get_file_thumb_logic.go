package filelogic

import (
	"context"
	"errors"
	"os"

	"onecloud/ent"
	"onecloud/internal/pkg/jwt"
	"onecloud/internal/pkg/response"
	"onecloud/internal/pkg/storage"
	"onecloud/internal/pkg/thumb"
	"onecloud/internal/repository"
	"onecloud/internal/svc"
	"onecloud/proto/.proto/service"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetFileThumbLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetFileThumbLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetFileThumbLogic {
	return &GetFileThumbLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// GetFileThumb 返回图片缩略图（JPEG）。
// 命中规则：仅本地驱动生成缩略图；缩略图缺失时按需自愈生成一次（原图需为本地驱动的图片）。
func (l *GetFileThumbLogic) GetFileThumb(in *service.FileThumbRequest) (*service.Response, error) {
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
			return response.ErrWithCode(response.CodeFileNotFound, "文件不存在")
		}
		return response.ErrInternal(l.Logger, "查询文件失败", err)
	}
	if f.UserID != userID {
		return response.ErrWithCode(response.CodeFileNoPermission, "无权访问该文件")
	}
	// 回收站中的文件不再提供缩略图
	if f.DeletedAt != nil {
		return response.ErrWithCode(response.CodeFileNotFound, "文件不存在")
	}
	if !thumb.IsSupported(f.Ext) {
		return response.ErrWithCode(response.CodeStorageNotImpl, "该文件类型不支持缩略图")
	}

	// 自愈：缩略图缺失（如历史文件）时按需生成一次
	thumbP := thumbPath(l.svcCtx, f.ID)
	if thumbP == "" {
		if _, ok := l.svcCtx.Storage.(*storage.LocalDriver); !ok {
			return response.ErrWithCode(response.CodeStorageNotImpl, "当前存储驱动不支持缩略图")
		}
		if !BuildThumb(l.ctx, l.svcCtx, f.ID, f.Path, f.Ext) {
			return response.ErrWithCode(response.CodeStorageRead, "缩略图生成失败")
		}
		thumbP = thumbPath(l.svcCtx, f.ID)
		if thumbP == "" {
			return response.ErrWithCode(response.CodeStorageRead, "缩略图生成失败")
		}
	}

	content, err := os.ReadFile(thumbP)
	if err != nil {
		return response.ErrWithCode(response.CodeStorageRead, "缩略图读取失败")
	}

	return response.Success(&service.DownloadFileResponse{
		Content: content,
		File: &service.FileMeta{
			FileId:      f.ID,
			Name:        f.Name,
			ContentType: "image/jpeg",
			Size:        int64(len(content)),
		},
	})
}
