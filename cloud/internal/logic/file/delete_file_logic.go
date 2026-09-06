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

	// 0. 事务前查出关联分享（提交后清理 Redis 用）
	shareRepo := repository.NewFileShareRepository(l.svcCtx.EntClient)
	shares, err := shareRepo.ListByFileIDs(l.ctx, []string{f.ID})
	if err != nil {
		l.Errorf("查询关联分享失败（继续删除）file=%s: %v", f.ID, err)
	}

	// 1. 事务删除：文件记录 + 关联分享 + 桶容量原子归还
	if err := fileRepo.DeleteFull(l.ctx, f); err != nil {
		return response.Err(errors.New("删除文件失败: " + err.Error()))
	}

	// 2. 清理 Redis：分享缓存、秒传索引
	shareCache := cache.NewShareCache(l.svcCtx.Redis)
	for _, s := range shares {
		if err := shareCache.Del(l.ctx, s.ShareCode); err != nil {
			l.Errorf("清理分享缓存失败 code=%s: %v", s.ShareCode, err)
		}
	}
	if f.Md5 != "" {
		md5Cache := cache.NewMD5Cache(l.svcCtx.Redis)
		if err := md5Cache.Del(l.ctx, userID, f.Md5, f.ID); err != nil {
			l.Errorf("清理 MD5 索引失败 user=%s md5=%s: %v", userID, f.Md5, err)
		}
	}

	// 3. 删除物理文件（失败仅告警，孤儿文件可被后续 GC 回收）
	if err := l.svcCtx.Storage.Delete(l.ctx, f.Path); err != nil {
		l.Errorf("物理文件删除失败（DB已删除）path=%s: %v", f.Path, err)
	}

	l.Infof("delete file success user=%s file=%s size=%d", userID, f.ID, f.Size)

	return response.Success(&service.DeleteFileResponse{Success: true})
}
