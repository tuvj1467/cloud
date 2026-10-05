package userlogic

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

type AdminPurgeFileLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewAdminPurgeFileLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AdminPurgeFileLogic {
	return &AdminPurgeFileLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

// AdminPurgeFile 管理员强制彻底删除任意文件（含正常/回收站文件）：
// 复用 DeleteFull 事务（级联分享 + 归还配额），事务后清理 Redis 与物理文件，写操作审计
func (l *AdminPurgeFileLogic) AdminPurgeFile(in *service.AdminPurgeFileRequest) (*service.Response, error) {
	if r, ok := AdminGuard(l.ctx); !ok {
		return r, nil
	}
	if err := in.Validate(); err != nil {
		return response.Err(errors.New("参数校验失败"))
	}

	adminID, _ := jwt.GetUserIdFromCtx(l.ctx)

	fileRepo := repository.NewFileRepository(l.svcCtx.EntClient)
	shareRepo := repository.NewFileShareRepository(l.svcCtx.EntClient)
	f, err := fileRepo.GetInfo(l.ctx, in.GetFileId())
	if err != nil {
		if ent.IsNotFound(err) {
			return response.ErrWithCode(response.CodeFileNotFound, "文件不存在")
		}
		return response.Err(errors.New("查询文件失败: " + err.Error()))
	}

	// 事务前查出关联分享（提交后清理 Redis 用）
	shares, err := shareRepo.ListByFileIDs(l.ctx, []string{f.ID})
	if err != nil {
		l.Errorf("查询关联分享失败（继续清除）file=%s: %v", f.ID, err)
	}

	if err := fileRepo.DeleteFull(l.ctx, f); err != nil {
		return response.Err(errors.New("强制删除失败: " + err.Error()))
	}

	// 事务后清理：分享缓存、秒传索引、物理文件（失败仅告警）
	shareCache := cache.NewShareCache(l.svcCtx.Redis)
	for _, s := range shares {
		if err := shareCache.Del(l.ctx, s.ShareCode); err != nil {
			l.Errorf("清理分享缓存失败 code=%s: %v", s.ShareCode, err)
		}
	}
	if f.Md5 != "" {
		md5Cache := cache.NewMD5Cache(l.svcCtx.Redis)
		if err := md5Cache.Del(l.ctx, f.UserID, f.Md5, f.ID); err != nil {
			l.Errorf("清理 MD5 索引失败: %v", err)
		}
	}
		// 共享路径守卫：秒传允许跨记录共享物理文件，仍有引用时跳过物理删除
		if err := fileRepo.DeletePhysicalIfUnshared(l.ctx, l.svcCtx.Storage, f.Path); err != nil {
			l.Errorf("物理文件删除失败（孤儿，待GC）path=%s: %v", f.Path, err)
		}

	// 操作审计（失败仅日志，不回滚主操作）
	auditRepo := repository.NewAdminAuditLogRepository(l.svcCtx.EntClient)
	if err := auditRepo.Create(l.ctx, adminID, "purge_file", f.ID, "owner="+f.UserID); err != nil {
		l.Errorf("写管理审计失败 admin=%s: %v", adminID, err)
	}

	l.Infof("admin purge file admin=%s file=%s owner=%s size=%d", adminID, f.ID, f.UserID, f.Size)
	return response.Success(&service.AdminPurgeFileResponse{Success: true})
}
