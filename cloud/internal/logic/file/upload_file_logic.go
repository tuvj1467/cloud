package filelogic

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"mime"
	"time"

	"onecloud/ent"
	"onecloud/internal/pkg/cache"
	"onecloud/internal/pkg/jwt"
	"onecloud/internal/pkg/response"
	"onecloud/internal/pkg/util"
	"onecloud/internal/repository"
	"onecloud/internal/svc"
	"onecloud/proto/.proto/service"

	"github.com/zeromicro/go-zero/core/logx"
)

type UploadFileLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewUploadFileLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UploadFileLogic {
	return &UploadFileLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// validate 校验上传入参
func (l *UploadFileLogic) validate(in *service.UploadFileRequest) error {
	if err := in.Validate(); err != nil {
		var vErr service.UploadFileRequestValidationError
		if errors.As(err, &vErr) {
			switch vErr.Field() {
			case "bucket_id":
				return errors.New("桶ID缺失")
			case "name":
				return errors.New("文件名为空或过长")
			case "suffix":
				return errors.New("缺少文件后缀")
			case "size":
				return errors.New("文件大小必须大于0")
			}
		}
		return errors.New("参数校验失败")
	}
	return nil
}

// UploadFile 上传文件
func (l *UploadFileLogic) UploadFile(in *service.UploadFileRequest) (*service.Response, error) {
	// 1. 校验入参
	if err := l.validate(in); err != nil {
		return response.Err(err)
	}

	// 2. 获取当前用户
	userID, err := jwt.GetUserIdFromCtx(l.ctx)
	if err != nil {
		return response.Err(err)
	}

	bucketRepo := repository.NewBucketRepository(l.svcCtx.EntClient)
	fileRepo := repository.NewFileRepository(l.svcCtx.EntClient)

	// 3. 校验桶归属
	b, err := bucketRepo.GetByID(l.ctx, in.GetBucketId())
	if err != nil {
		if ent.IsNotFound(err) {
			return response.ErrWithCode(response.CodeBucketNotFound, "桶不存在")
		}
		return response.Err(errors.New("查询桶失败: " + err.Error()))
	}
	if b.UserID != userID {
		return response.ErrWithCode(response.CodeBucketNoPermission, "无权操作该桶")
	}

	// 4. 校验桶容量
	if b.UsedSize+in.GetSize() > b.SizeLimit {
		return response.ErrWithCode(response.CodeBucketCapacity, "桶容量不足")
	}

	// 5. 计算 MD5
	md5 := util.CalculateFileMD5(in.GetContent())

	// 6. 秒传检查：优先查 Redis 索引，未命中回退 DB
	md5Cache := cache.NewMD5Cache(l.svcCtx.Redis)
	if cachedFileID, _ := md5Cache.Get(l.ctx, userID, md5); cachedFileID != "" {
		// 命中 Redis，校验文件仍存在
		if exist, _ := fileRepo.GetInfo(l.ctx, cachedFileID); exist != nil {
			l.Infof("instant hit (redis) user=%s md5=%s file=%s", userID, md5, cachedFileID)
			return response.Success(&service.UploadFileResponse{
				File:    toFileMeta(exist),
				Instant: true,
			})
		}
		// Redis 索引指向的文件已不存在（脏数据），清理后继续
		_ = md5Cache.Del(l.ctx, userID, md5, cachedFileID)
	}

	// 6.1 Redis 未命中，回退 DB 查询
	if exist, _ := fileRepo.GetByMD5(l.ctx, userID, md5); exist != nil {
		// DB 命中后回写 Redis，重建索引
		_ = md5Cache.Set(l.ctx, userID, md5, exist.ID)
		l.Infof("instant hit (db) user=%s md5=%s file=%s", userID, md5, exist.ID)
		return response.Success(&service.UploadFileResponse{
			File:    toFileMeta(exist),
			Instant: true,
		})
	}

	// 7. 生成文件ID与存储路径
	fileID := l.svcCtx.Snowflake.MustIdS()
	date := time.Now().Format("20060102")
	filePath := fmt.Sprintf("%s/%s/%s.%s", userID, date, fileID, in.GetSuffix())
	contentType := mime.TypeByExtension("." + in.GetSuffix())
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	// 8. 通过存储驱动写入物理文件
	if err := l.svcCtx.Storage.Put(l.ctx, filePath, bytes.NewReader(in.GetContent()), in.GetSize()); err != nil {
		return response.Err(errors.New("文件存储失败: " + err.Error()))
	}

	// 9. 写入文件元信息
	res, err := fileRepo.Create(l.ctx, in, fileID, userID, in.GetBucketId(), filePath, contentType, md5)
	if err != nil {
		// 写库失败则回滚物理文件
		_ = l.svcCtx.Storage.Delete(l.ctx, filePath)
		return response.Err(errors.New("文件入库失败: " + err.Error()))
	}

	// 9.1 写入 Redis MD5 索引，便于后续秒传
	if err := md5Cache.Set(l.ctx, userID, md5, res.ID); err != nil {
		l.Errorf("写入 MD5 索引失败（不影响主流程）: %v", err)
	}

	// 10. 扣减桶容量（乐观锁）
	if err := bucketRepo.IncUsedSize(l.ctx, in.GetBucketId(), in.GetSize()); err != nil {
		l.Errorf("桶容量更新失败（文件已入库）: %v", err)
	}

	l.Infof("upload success user=%s bucket=%s file=%s size=%d md5=%s",
		userID, in.GetBucketId(), res.ID, in.GetSize(), md5)

	return response.Success(&service.UploadFileResponse{
		File:    toFileMeta(res),
		Instant: false,
	})
}
