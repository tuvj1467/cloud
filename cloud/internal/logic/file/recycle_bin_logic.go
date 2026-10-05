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

// ===== 回收站列表 =====

type ListDeletedFilesLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListDeletedFilesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListDeletedFilesLogic {
	return &ListDeletedFilesLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *ListDeletedFilesLogic) ListDeletedFiles(in *service.ListDeletedFilesRequest) (*service.Response, error) {
	if err := in.Validate(); err != nil {
		return response.Err(errors.New("参数校验失败"))
	}
	userID, err := jwt.GetUserIdFromCtx(l.ctx)
	if err != nil {
		return response.Err(err)
	}

	fileRepo := repository.NewFileRepository(l.svcCtx.EntClient)
	list, total, err := fileRepo.ListDeleted(l.ctx, userID, int(in.GetPage()), int(in.GetPageSize()))
	if err != nil {
		return response.ErrInternal(l.Logger, "查询回收站失败", err)
	}

	items := make([]*service.DeletedFileItem, 0, len(list))
	for _, f := range list {
		item := &service.DeletedFileItem{
			FileId:   f.ID,
			Name:     f.Name,
			Size:     f.Size,
			BucketId: f.BucketID,
		}
		if f.DeletedAt != nil {
			item.DeletedAt = f.DeletedAt.Format("2006-01-02 15:04:05")
		}
		items = append(items, item)
	}
	return response.Success(&service.ListDeletedFilesResponse{Files: items, Total: int32(total)})
}

// ===== 批量恢复 =====

type RestoreFilesLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewRestoreFilesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RestoreFilesLogic {
	return &RestoreFilesLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *RestoreFilesLogic) RestoreFiles(in *service.RestoreFilesRequest) (*service.Response, error) {
	if err := in.Validate(); err != nil {
		return response.Err(errors.New("参数校验失败"))
	}
	userID, err := jwt.GetUserIdFromCtx(l.ctx)
	if err != nil {
		return response.Err(err)
	}

	fileRepo := repository.NewFileRepository(l.svcCtx.EntClient)
	restored, err := fileRepo.RestoreByIDs(l.ctx, userID, in.GetFileIds())
	if err != nil {
		return response.ErrInternal(l.Logger, "恢复文件失败", err)
	}
	l.Infof("recycle restore user=%s requested=%d restored=%d", userID, len(in.GetFileIds()), restored)
	return response.Success(&service.RestoreFilesResponse{RestoredCount: int32(restored)})
}

// ===== 批量彻底删除 =====

type PurgeFilesLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewPurgeFilesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *PurgeFilesLogic {
	return &PurgeFilesLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

// PurgeFiles 彻底删除本人回收站中的一组文件：
// 逐个复用 DeleteFull 事务（级联分享 + 归还配额），事务后清理 Redis 与物理文件；
// 不属于本人或已不在回收站的 ID 自动跳过
func (l *PurgeFilesLogic) PurgeFiles(in *service.PurgeFilesRequest) (*service.Response, error) {
	if err := in.Validate(); err != nil {
		return response.Err(errors.New("参数校验失败"))
	}
	userID, err := jwt.GetUserIdFromCtx(l.ctx)
	if err != nil {
		return response.Err(err)
	}

	fileRepo := repository.NewFileRepository(l.svcCtx.EntClient)
	shareRepo := repository.NewFileShareRepository(l.svcCtx.EntClient)
	targets, err := fileRepo.ListDeletedByIDs(l.ctx, userID, in.GetFileIds())
	if err != nil {
		return response.ErrInternal(l.Logger, "查询回收站文件失败", err)
	}

	// 批量预取全部目标的关联分享（原逐文件单查，N+1）；fileID → shares
	targetIDs := make([]string, 0, len(targets))
	for _, f := range targets {
		targetIDs = append(targetIDs, f.ID)
	}
	sharesByFile := map[string][]*ent.FileShare{}
	if len(targetIDs) > 0 {
		allShares, err := shareRepo.ListByFileIDs(l.ctx, targetIDs)
		if err != nil {
			l.Errorf("批量查询关联分享失败（继续清除）: %v", err)
		}
		for _, s := range allShares {
			sharesByFile[s.FileID] = append(sharesByFile[s.FileID], s)
		}
	}
	shareCache := cache.NewShareCache(l.svcCtx.Redis)
	md5Cache := cache.NewMD5Cache(l.svcCtx.Redis)

	purged := 0
	for _, f := range targets {
		shares := sharesByFile[f.ID]

		// 事务：删分享 + 删记录 + 归还配额（回收站占用随彻底删除释放）
		if err := fileRepo.DeleteFull(l.ctx, f); err != nil {
			l.Errorf("彻底删除文件失败 file=%s: %v", f.ID, err)
			continue
		}

		// 事务后清理：分享缓存、秒传索引、物理文件（失败仅告警）
		for _, s := range shares {
			if err := shareCache.Del(l.ctx, s.ShareCode); err != nil {
				l.Errorf("清理分享缓存失败 code=%s: %v", s.ShareCode, err)
			}
		}
		if f.Md5 != "" {
			if err := md5Cache.Del(l.ctx, userID, f.Md5, f.ID); err != nil {
				l.Errorf("清理 MD5 索引失败: %v", err)
			}
		}
		// 共享路径守卫：秒传允许跨记录共享物理文件，仍有引用时跳过物理删除
		if err := fileRepo.DeletePhysicalIfUnshared(l.ctx, l.svcCtx.Storage, f.Path); err != nil {
			l.Errorf("物理文件删除失败（孤儿，待GC）path=%s: %v", f.Path, err)
		}
		purged++
	}

	l.Infof("recycle purge user=%s requested=%d purged=%d", userID, len(in.GetFileIds()), purged)
	return response.Success(&service.PurgeFilesResponse{PurgedCount: int32(purged)})
}
