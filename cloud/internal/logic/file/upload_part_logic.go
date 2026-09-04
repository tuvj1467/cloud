package filelogic

import (
	"bytes"
	"context"
	"errors"

	"onecloud/internal/pkg/cache"
	"onecloud/internal/pkg/jwt"
	"onecloud/internal/pkg/response"
	"onecloud/internal/repository"
	"onecloud/internal/svc"
	"onecloud/proto/.proto/service"

	"github.com/zeromicro/go-zero/core/logx"
)

type UploadPartLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewUploadPartLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UploadPartLogic {
	return &UploadPartLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// UploadPart 上传单个分片
// 步骤：校验入参 → 校验 upload_id（Redis meta，未命中查 DB 兜底）
//
//	→ 校验 part_number 范围 → 检查是否已上传（断点续传幂等）
//	→ 调用驱动 UploadPart → 更新 DB → 写 Redis parts hash
func (l *UploadPartLogic) UploadPart(in *service.UploadPartRequest) (*service.Response, error) {
	// 1. 校验入参
	if err := in.Validate(); err != nil {
		return response.Err(errors.New("参数校验失败: " + err.Error()))
	}

	// 2. 获取当前用户
	userID, err := jwt.GetUserIdFromCtx(l.ctx)
	if err != nil {
		return response.Err(err)
	}

	partRepo := repository.NewFilePartRepository(l.svcCtx.EntClient)
	multipartCache := cache.NewMultipartCache(l.svcCtx.Redis)

	// 3. 读取上传元信息（Redis 优先，DB 兜底）
	meta, err := multipartCache.GetMeta(l.ctx, in.GetUploadId())
	if err != nil {
		return response.Err(errors.New("查询上传元信息失败: " + err.Error()))
	}
	if meta == nil {
		// Redis 未命中，查 DB 兜底
		part, err := partRepo.GetMetaByUploadID(l.ctx, in.GetUploadId())
		if err != nil {
			return response.Err(errors.New("上传任务不存在或已过期: " + err.Error()))
		}
		// 重建 Redis meta
		meta = &cache.MultipartMeta{
			UploadID:   in.GetUploadId(),
			UserID:     part.UserID,
			BucketID:   part.BucketID,
			FileName:   part.FileName,
			FileSuffix: part.FileSuffix,
			FileSize:   part.FileSize,
			TotalParts: part.TotalParts,
			StorageID:  in.GetUploadId(), // 本地驱动用 upload_id 作为 storageID
		}
		_ = multipartCache.SetMeta(l.ctx, meta)
	}

	// 4. 校验归属
	if meta.UserID != userID {
		return response.Err(errors.New("无权操作该上传任务"))
	}

	// 5. 校验 part_number 范围
	if int(in.GetPartNumber()) < 1 || int(in.GetPartNumber()) > meta.TotalParts {
		return response.Err(errors.New("分片序号超出范围"))
	}

	// 6. 断点续传：检查是否已上传
	if existingMD5, hit, _ := multipartCache.GetPart(l.ctx, in.GetUploadId(), int(in.GetPartNumber())); hit {
		// 幂等返回，不重复写物理文件
		return response.Success(&service.UploadPartResponse{
			Success: true,
			PartMd5: existingMD5,
		})
	}

	// 7. 调用存储驱动上传分片（用 storage_id 而非业务 upload_id）
	partMD5, err := l.svcCtx.Storage.UploadPart(
		l.ctx,
		meta.StorageID,
		int(in.GetPartNumber()),
		bytes.NewReader(in.GetContent()),
		in.GetSize(),
	)
	if err != nil {
		return response.Err(errors.New("分片写入失败: " + err.Error()))
	}

	// 8. 更新 DB 分片记录
	part, err := partRepo.GetByUploadAndNumber(l.ctx, in.GetUploadId(), int(in.GetPartNumber()))
	if err != nil {
		// DB 记录不存在（可能被异步清理），尝试创建一条
		l.Errorf("查询分片记录失败 upload_id=%s part=%d: %v", in.GetUploadId(), in.GetPartNumber(), err)
		return response.Err(errors.New("分片记录不存在: " + err.Error()))
	}
	if err := partRepo.MarkUploaded(l.ctx, part.ID, in.GetSize(), partMD5, ""); err != nil {
		l.Errorf("更新分片状态失败 part_id=%s: %v", part.ID, err)
	}

	// 9. 写入 Redis parts hash
	if _, err := multipartCache.AddPart(l.ctx, in.GetUploadId(), int(in.GetPartNumber()), partMD5); err != nil {
		l.Errorf("写入分片 Redis 状态失败: %v", err)
	}

	l.Infof("upload part success user=%s upload_id=%s part=%d size=%d md5=%s",
		userID, in.GetUploadId(), in.GetPartNumber(), in.GetSize(), partMD5)

	return response.Success(&service.UploadPartResponse{
		Success: true,
		PartMd5: partMD5,
	})
}
