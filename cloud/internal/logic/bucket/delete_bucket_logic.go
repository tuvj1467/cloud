package bucketlogic

import (
	"context"
	"errors"

	"onecloud/ent"
	"onecloud/ent/file"
	"onecloud/internal/pkg/cache"
	"onecloud/internal/pkg/jwt"
	"onecloud/internal/pkg/response"
	"onecloud/internal/repository"
	"onecloud/internal/svc"
	"onecloud/proto/.proto/service"

	"github.com/zeromicro/go-zero/core/logx"
)

type DeleteBucketLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewDeleteBucketLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteBucketLogic {
	return &DeleteBucketLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// DeleteBucket 删除桶（非 force 模式下桶必须为空）
func (l *DeleteBucketLogic) DeleteBucket(in *service.DeleteBucketRequest) (*service.Response, error) {
	if err := in.Validate(); err != nil {
		return response.Err(errors.New("参数校验失败: " + err.Error()))
	}

	userID, err := jwt.GetUserIdFromCtx(l.ctx)
	if err != nil {
		return response.Err(err)
	}

	bucketRepo := repository.NewBucketRepository(l.svcCtx.EntClient)
	b, err := bucketRepo.GetByID(l.ctx, in.GetBucketId())
	if err != nil {
		if ent.IsNotFound(err) {
			return response.ErrWithCode(response.CodeBucketNotFound, "桶不存在")
		}
		return response.ErrInternal(l.Logger, "查询桶失败", err)
	}

	// 校验桶归属
	if b.UserID != userID {
		return response.ErrWithCode(response.CodeBucketNoPermission, "无权删除该桶")
	}

	shareRepo := repository.NewFileShareRepository(l.svcCtx.EntClient)
	fileRepo := repository.NewFileRepository(l.svcCtx.EntClient)

	if in.GetForce() {
		// force：先查出文件关联分享（供事务后清理 Redis），再事务级联删除
		files, err := l.svcCtx.EntClient.File.Query().Where(file.BucketIDEQ(in.GetBucketId())).All(l.ctx)
		if err != nil {
			return response.ErrInternal(l.Logger, "查询桶内文件失败", err)
		}
		fileIDs := make([]string, 0, len(files))
		for _, f := range files {
			fileIDs = append(fileIDs, f.ID)
		}
		var shares []*ent.FileShare
		if len(fileIDs) > 0 {
			shares, err = shareRepo.ListByFileIDs(l.ctx, fileIDs)
			if err != nil {
				l.Errorf("查询关联分享失败（继续删除）: %v", err)
			}
		}

		deletedFiles, err := bucketRepo.DeleteWithFiles(l.ctx, in.GetBucketId())
		if err != nil {
			return response.ErrInternal(l.Logger, "强制删除桶失败", err)
		}

		// 事务后清理：分享缓存、秒传索引、物理文件
		shareCache := cache.NewShareCache(l.svcCtx.Redis)
		md5Cache := cache.NewMD5Cache(l.svcCtx.Redis)
		for _, s := range shares {
			_ = shareCache.Del(l.ctx, s.ShareCode)
		}
		removed := 0
		for _, f := range deletedFiles {
			if f.Md5 != "" {
				_ = md5Cache.Del(l.ctx, f.UserID, f.Md5, f.ID)
			}
			// 共享路径守卫：秒传允许跨记录共享物理文件，仍有引用时跳过物理删除
			if err := fileRepo.DeletePhysicalIfUnshared(l.ctx, l.svcCtx.Storage, f.Path); err != nil {
				l.Errorf("物理文件删除失败（孤儿，待GC）file=%s path=%s: %v", f.ID, f.Path, err)
				continue
			}
			removed++
		}
		l.Infof("force delete bucket user=%s bucket=%s files=%d physical_removed=%d", userID, in.GetBucketId(), len(deletedFiles), removed)
		return response.Success(&service.DeleteBucketResponse{Success: true})
	}

	// 非 force：事务内校验桶为空后删除
	empty, err := bucketRepo.DeleteIfEmpty(l.ctx, in.GetBucketId())
	if err != nil {
		return response.ErrInternal(l.Logger, "删除桶失败", err)
	}
	if !empty {
		return response.ErrWithCode(response.CodeBucketNotEmpty, "桶非空，无法删除（可使用 force 强制删除）")
	}

	return response.Success(&service.DeleteBucketResponse{Success: true})
}
