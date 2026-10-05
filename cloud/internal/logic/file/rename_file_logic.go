package filelogic

import (
	"context"
	"errors"

	"onecloud/ent"
	"onecloud/ent/file"
	"onecloud/internal/pkg/jwt"
	"onecloud/internal/pkg/response"
	"onecloud/internal/repository"
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

// RenameFile 重命名文件（同桶内文件名不可重复）
func (l *RenameFileLogic) RenameFile(in *service.RenameFileRequest) (*service.Response, error) {
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
		return response.Err(errors.New("无权重命名该文件"))
	}
	// 回收站中的文件不可重命名（还原后操作）
	if f.DeletedAt != nil {
		return response.Err(errors.New("文件已在回收站中，请先还原"))
	}

	// 校验同桶下文件名是否冲突
	if f.Name != in.GetNewName() {
		conflict, err := l.svcCtx.EntClient.File.Query().
			Where(
				file.BucketIDEQ(f.BucketID),
				file.NameEQ(in.GetNewName()),
				file.IDNEQ(f.ID),
			).
			First(l.ctx)
		if err != nil && !ent.IsNotFound(err) {
			return response.ErrInternal(l.Logger, "查询文件名冲突失败", err)
		}
		if conflict != nil {
			return response.Err(errors.New("同桶下已存在同名文件"))
		}
	}

	// 执行重命名
	updated, err := fileRepo.Rename(l.ctx, in.GetFileId(), in.GetNewName())
	if err != nil {
		return response.ErrInternal(l.Logger, "重命名失败", err)
	}

	_ = updated // 重命名后的文件信息（如需返回可使用 toFileMeta(updated)）
	return response.Success(&service.RenameFileResponse{Success: true})
}
