package filelogic

import (
	"bytes"
	"context"
	"errors"

	"onecloud/ent/filepart"
	"onecloud/internal/pkg/cache"
	"onecloud/internal/pkg/jwt"
	"onecloud/internal/pkg/response"
	"onecloud/internal/pkg/storage"
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

	// upload_id 形态白名单（DB 兜底时它直接作为存储目录名）
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
			return response.ErrInternal(l.Logger, "上传任务不存在或已过期", err)
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

	// 大小治理：分片以服务端 len(content) 为准，且不得超过单分片上限（与简单上传同一配置）
	partSize := int64(len(in.GetContent()))
	if partSize == 0 {
		return response.Err(errors.New("分片内容为空"))
	}
	if partSize != in.GetSize() {
		return response.Err(errors.New("分片大小与内容不一致，请重新上传"))
	}
	if limit := l.svcCtx.Config.Storage.MaxUploadSize; limit > 0 && partSize > limit {
		return response.ErrWithCode(response.CodeFileTooLarge, "分片超过大小上限")
	}

	// 5. 校验 part_number 范围
	if int(in.GetPartNumber()) < 1 || int(in.GetPartNumber()) > meta.TotalParts {
		return response.Err(errors.New("分片序号超出范围"))
	}

	// 6. 断点续传：检查是否已上传
	if existingMD5, hit, _ := multipartCache.GetPart(l.ctx, in.GetUploadId(), int(in.GetPartNumber())); hit {
		// Redis 命中不直接信任：若 DB 记录仍是 pending（历史「MarkUploaded 失败但 Redis 已写」
		// 留下的状态分叉），先修复 DB——否则 Complete 的 CountUploaded 永远不满足，
		// 上传卡死且重传被 Redis 幂等短路，直至 24h TTL 过期
		if part, qErr := partRepo.GetByUploadAndNumber(l.ctx, in.GetUploadId(), int(in.GetPartNumber())); qErr == nil && part.Status != filepart.StatusUploaded {
			if uErr := partRepo.MarkUploaded(l.ctx, part.ID, in.GetSize(), existingMD5, ""); uErr != nil {
				l.Errorf("修复分片 DB 状态失败 upload_id=%s part=%d: %v", in.GetUploadId(), in.GetPartNumber(), uErr)
			}
		}
		// 幂等返回，不重复写物理文件
		return response.Success(&service.UploadPartResponse{
			Success: true,
			PartMd5: existingMD5,
		})
	}

	// 7. 调用存储驱动上传分片（upload_id 即存储目录名，ID 已统一）
	partMD5, err := l.svcCtx.Storage.UploadPart(
		l.ctx,
		meta.StorageID,
		int(in.GetPartNumber()),
		bytes.NewReader(in.GetContent()),
		in.GetSize(),
	)
	if err != nil {
		return response.ErrInternal(l.Logger, "分片写入失败", err)
	}

	// 8. 更新 DB 分片记录（DB 是 Complete 完整性判定的基准，必须先于 Redis 成功）
	part, err := partRepo.GetByUploadAndNumber(l.ctx, in.GetUploadId(), int(in.GetPartNumber()))
	if err != nil {
		l.Errorf("查询分片记录失败 upload_id=%s part=%d: %v", in.GetUploadId(), in.GetPartNumber(), err)
		return response.Err(errors.New("分片记录不存在或已过期"))
	}
	if err := partRepo.MarkUploaded(l.ctx, part.ID, in.GetSize(), partMD5, ""); err != nil {
		// 失败直接报错且不写 Redis——若此处吞错误继续写 Redis，重传会被 Redis 幂等命中
		// 短路返回成功，DB 永远停在 pending，上传将永久卡死
		l.Errorf("更新分片状态失败 part_id=%s: %v", part.ID, err)
		return response.Err(errors.New("分片状态记录失败，请重试该分片"))
	}

	// 9. 写入 Redis parts hash
	if _, err := multipartCache.AddPart(l.ctx, in.GetUploadId(), int(in.GetPartNumber()), partMD5); err != nil {
		// Redis 未写入仅损失断点续传幂等与逐片校验（DB 已记录），不影响正确性
		l.Errorf("写入分片 Redis 状态失败: %v", err)
	}

	l.Infof("upload part success user=%s upload_id=%s part=%d size=%d md5=%s",
		userID, in.GetUploadId(), in.GetPartNumber(), in.GetSize(), partMD5)

	return response.Success(&service.UploadPartResponse{
		Success: true,
		PartMd5: partMD5,
	})
}
