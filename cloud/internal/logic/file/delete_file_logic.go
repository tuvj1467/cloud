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
	bucketRepo := repository.NewBucketRepository(l.svcCtx.EntClient)

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

	// 1. 删除 DB 记录
	if err := fileRepo.Delete(l.ctx, in.GetFileId()); err != nil {
		return response.Err(errors.New("删除文件记录失败: " + err.Error()))
	}

	// 2. 删除物理文件（失败仅记录日志，不阻塞主流程，避免孤儿记录阻塞删除）
	if err := l.svcCtx.Storage.Delete(l.ctx, f.Path); err != nil {
		l.Errorf("物理文件删除失败（DB已删除）path=%s: %v", f.Path, err)
	}

	// 2.1 清理 Redis MD5 索引（仅当 value 等于被删 file_id 时删，防误删）
	if f.Md5 != "" {
		md5Cache := cache.NewMD5Cache(l.svcCtx.Redis)
		if err := md5Cache.Del(l.ctx, userID, f.Md5, f.ID); err != nil {
			l.Errorf("清理 MD5 索引失败 user=%s md5=%s: %v", userID, f.Md5, err)
		}
	}

	// 3. 回收桶容量（乐观锁，失败仅告警）
	if f.BucketID != "" {
		if err := bucketRepo.IncUsedSize(l.ctx, f.BucketID, -f.Size); err != nil {
			l.Errorf("桶容量回收失败 bucket=%s: %v", f.BucketID, err)
		}
	}

	l.Infof("delete file success user=%s file=%s size=%d", userID, f.ID, f.Size)

	return response.Success(&service.DeleteFileResponse{Success: true})
}
