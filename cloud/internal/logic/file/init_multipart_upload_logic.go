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

type InitMultipartUploadLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewInitMultipartUploadLogic(ctx context.Context, svcCtx *svc.ServiceContext) *InitMultipartUploadLogic {
	return &InitMultipartUploadLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// InitMultipartUpload 初始化分片上传
// 步骤：校验入参 → 校验桶归属与容量 → 生成 upload_id → 调用驱动 InitPart
//
//	→ 写 Redis meta → 批量创建 FilePart 记录 → 返回 upload_id
func (l *InitMultipartUploadLogic) InitMultipartUpload(in *service.InitMultipartUploadRequest) (*service.Response, error) {
	// 1. 校验入参
	if err := in.Validate(); err != nil {
		return response.Err(errors.New("参数校验失败: " + err.Error()))
	}

	// 2. 获取当前用户
	userID, err := jwt.GetUserIdFromCtx(l.ctx)
	if err != nil {
		return response.Err(err)
	}

	bucketRepo := repository.NewBucketRepository(l.svcCtx.EntClient)
	fileRepo := repository.NewFileRepository(l.svcCtx.EntClient)
	partRepo := repository.NewFilePartRepository(l.svcCtx.EntClient)

	// 3. 校验桶归属
	b, err := bucketRepo.GetByID(l.ctx, in.GetBucketId())
	if err != nil {
		if ent.IsNotFound(err) {
			return response.Err(errors.New("桶不存在"))
		}
		return response.Err(errors.New("查询桶失败: " + err.Error()))
	}
	if b.UserID != userID {
		return response.Err(errors.New("无权操作该桶"))
	}

	// 4. 校验桶容量
	if b.UsedSize+in.GetSize() > b.SizeLimit {
		return response.Err(errors.New("桶容量不足"))
	}

	// 5. 秒传检查：若客户端已知 MD5，先查询是否已存在
	if in.GetMd5() != "" {
		if exist, _ := fileRepo.GetByMD5(l.ctx, userID, in.GetMd5()); exist != nil {
			l.Infof("instant hit (multipart-init) user=%s md5=%s file=%s", userID, in.GetMd5(), exist.ID)
			return response.Success(&service.InitMultipartUploadResponse{
				UploadId: "",
				PartSize: 0,
			})
		}
	}

	// 6. 生成 upload_id（雪花ID，便于 DB 索引）
	uploadID := l.svcCtx.Snowflake.MustIdS()

	// 7. 调用存储驱动初始化分片上传
	storageUploadID, err := l.svcCtx.Storage.InitPart(l.ctx, "", int(in.GetTotalParts()))
	if err != nil {
		return response.Err(errors.New("初始化分片上传失败: " + err.Error()))
	}

	// 8. 写入 Redis 元信息
	multipartCache := cache.NewMultipartCache(l.svcCtx.Redis)
	meta := &cache.MultipartMeta{
		UploadID:   uploadID,
		UserID:     userID,
		BucketID:   in.GetBucketId(),
		FileName:   in.GetName(),
		FileSuffix: in.GetSuffix(),
		FileSize:   in.GetSize(),
		TotalParts: int(in.GetTotalParts()),
		StorageID:  storageUploadID,
	}
	if err := multipartCache.SetMeta(l.ctx, meta); err != nil {
		l.Errorf("写入分片上传元信息到 Redis 失败（继续走 DB 兜底）: %v", err)
	}

	// 9. 批量创建 FilePart 记录（status=pending）
	if err := partRepo.CreateBatch(l.ctx, uploadID, userID, in.GetBucketId(), in); err != nil {
		// 写库失败，回滚物理初始化
		_ = l.svcCtx.Storage.AbortPart(l.ctx, storageUploadID)
		_ = multipartCache.Clear(l.ctx, uploadID)
		return response.Err(errors.New("创建分片记录失败: " + err.Error()))
	}

	// 10. 计算建议分片大小
	partSize := int32(in.GetSize() / int64(in.GetTotalParts()))
	if in.GetSize()%int64(in.GetTotalParts()) != 0 {
		partSize++
	}

	l.Infof("multipart init success user=%s bucket=%s upload_id=%s total_parts=%d size=%d",
		userID, in.GetBucketId(), uploadID, in.GetTotalParts(), in.GetSize())

	return response.Success(&service.InitMultipartUploadResponse{
		UploadId: uploadID,
		PartSize: partSize,
	})
}
