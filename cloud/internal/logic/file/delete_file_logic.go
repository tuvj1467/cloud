package filelogic

import (
	"context"
	"errors"

	"onecloud/ent"
	"onecloud/internal/pkg/cache"
	"onecloud/internal/pkg/jwt"
	"onecloud/internal/pkg/response"
	"onecloud/internal/repository"
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

// DeleteFile 删除文件（DB 记录 + 物理文件 + 回收桶容量）
func (l *DeleteFileLogic) DeleteFile(in *service.DeleteFileRequest) (*service.Response, error) {
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
		return response.Err(errors.New("查询文件失败: " + err.Error()))
	}

	// 校验文件归属
	if f.UserID != userID {
		return response.ErrWithCode(response.CodeFileNoPermission, "无权删除该文件")
	}

	// 软删除：仅标记 deleted_at（回收站语义）
	// 配额不归还（回收站占用容量）、分享不级联、物理文件保留——彻底删除（Purge）时才处理
	if err := fileRepo.SoftDelete(l.ctx, f.ID); err != nil {
		return response.Err(errors.New("删除文件失败: " + err.Error()))
	}

	// 清理秒传索引：同内容再次上传应创建新文件，而不是命中回收站里的记录
	if f.Md5 != "" {
		md5Cache := cache.NewMD5Cache(l.svcCtx.Redis)
		if err := md5Cache.Del(l.ctx, userID, f.Md5, f.ID); err != nil {
			l.Errorf("清理 MD5 索引失败 user=%s md5=%s: %v", userID, f.Md5, err)
		}
	}

	l.Infof("file moved to recycle bin user=%s file=%s size=%d", userID, f.ID, f.Size)

	return response.Success(&service.DeleteFileResponse{Success: true})
}
