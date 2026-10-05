package filelogic

import (
	"context"
	"errors"

	"onecloud/internal/pkg/cache"
	"onecloud/internal/pkg/jwt"
	"onecloud/internal/pkg/response"
	"onecloud/internal/pkg/storage"
	"onecloud/internal/repository"
	"onecloud/internal/svc"
	"onecloud/proto/.proto/service"

	"github.com/zeromicro/go-zero/core/logx"
)

type AbortMultipartUploadLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewAbortMultipartUploadLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AbortMultipartUploadLogic {
	return &AbortMultipartUploadLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// AbortMultipartUpload 取消分片上传
// 步骤：校验 upload_id 归属 → 调用驱动 AbortPart 清理物理分片
//
//	→ 删除 DB 分片记录 → 清理 Redis meta/parts
func (l *AbortMultipartUploadLogic) AbortMultipartUpload(in *service.AbortMultipartUploadRequest) (*service.Response, error) {
	// 1. 校验入参
	if err := in.Validate(); err != nil {
		return response.Err(errors.New("参数校验失败: " + err.Error()))
	}

	// 2. 获取当前用户
	userID, err := jwt.GetUserIdFromCtx(l.ctx)
	if err != nil {
		return response.Err(err)
	}

	// upload_id 形态白名单（它直接作为存储目录名）
	if !storage.ValidStorageID(in.GetUploadId()) {
		return response.ErrWithCode(response.CodeStoragePathInvalid, "upload_id 非法")
	}

	partRepo := repository.NewFilePartRepository(l.svcCtx.EntClient)
	multipartCache := cache.NewMultipartCache(l.svcCtx.Redis)

	// 3. 读取上传元信息（Redis 优先，DB 兜底）
	meta, err := multipartCache.GetMeta(l.ctx, in.GetUploadId())
	if err != nil {
		return response.ErrInternal(l.Logger, "查询上传元信息失败", err)
	}
	if meta == nil {
		// Redis 未命中，查 DB 兜底
		part, err := partRepo.GetMetaByUploadID(l.ctx, in.GetUploadId())
		if err != nil {
			return response.Err(errors.New("上传任务不存在或已过期"))
		}
		meta = &cache.MultipartMeta{
			UploadID:  in.GetUploadId(),
			UserID:    part.UserID,
			StorageID: in.GetUploadId(),
		}
	}

	// 4. 校验归属
	if meta.UserID != userID {
		return response.Err(errors.New("无权操作该上传任务"))
	}

	// 5. 调用驱动清理物理分片
	// 「任务不存在」视为已清理，继续删 DB/Redis；
	// 其他失败（存储端瞬时故障）直接返回错误——此时删 DB 记录会让服务端
	// 残留的任务/对象成为无法通过 API 触达的孤儿，保留记录供客户端重试中止
	if err := l.svcCtx.Storage.AbortPart(l.ctx, meta.StorageID); err != nil && !errors.Is(err, storage.ErrFileNotFound) {
		return response.ErrInternal(l.Logger, "清理物理分片失败，请稍后重试", err)
	}

	// 6. 删除 DB 分片记录
	if err := partRepo.DeleteByUploadID(l.ctx, in.GetUploadId()); err != nil {
		l.Errorf("删除分片记录失败 upload_id=%s: %v", in.GetUploadId(), err)
	}

	// 7. 清理 Redis meta/parts
	if err := multipartCache.Clear(l.ctx, in.GetUploadId()); err != nil {
		l.Errorf("清理 Redis 上传状态失败 upload_id=%s: %v", in.GetUploadId(), err)
	}

	l.Infof("multipart abort success user=%s upload_id=%s", userID, in.GetUploadId())

	return response.Success(&service.AbortMultipartUploadResponse{
		Success: true,
	})
}
