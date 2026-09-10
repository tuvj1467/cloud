package filelogic

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"time"

	"onecloud/internal/pkg/cache"
	"onecloud/internal/pkg/jwt"
	"onecloud/internal/pkg/response"
	"onecloud/internal/pkg/storage"
	"onecloud/internal/repository"
	"onecloud/internal/svc"
	"onecloud/proto/.proto/service"

	"github.com/zeromicro/go-zero/core/logx"
)

type CompleteMultipartUploadLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCompleteMultipartUploadLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CompleteMultipartUploadLogic {
	return &CompleteMultipartUploadLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// CompleteMultipartUpload 完成分片上传
// 步骤：校验 upload_id → 校验分片完整性 → 生成目标路径 → 调用驱动 CompletePart 合并
//
//	→ 计算合并后 MD5 → 秒传检查 → 创建 File 元数据 → 更新桶容量
//	→ 写 Redis MD5 索引 → 更新分片状态为 merged → 清理 Redis meta/parts
func (l *CompleteMultipartUploadLogic) CompleteMultipartUpload(in *service.CompleteMultipartUploadRequest) (*service.Response, error) {
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

	bucketRepo := repository.NewBucketRepository(l.svcCtx.EntClient)
	fileRepo := repository.NewFileRepository(l.svcCtx.EntClient)
	partRepo := repository.NewFilePartRepository(l.svcCtx.EntClient)
	multipartCache := cache.NewMultipartCache(l.svcCtx.Redis)
	md5Cache := cache.NewMD5Cache(l.svcCtx.Redis)

	// 3. 读取上传元信息（Redis 优先，DB 兜底）
	meta, err := multipartCache.GetMeta(l.ctx, in.GetUploadId())
	if err != nil {
		return response.Err(errors.New("查询上传元信息失败: " + err.Error()))
	}
	if meta == nil {
		// Redis 未命中，查 DB 兜底
		part, err := partRepo.GetMetaByUploadID(l.ctx, in.GetUploadId())
		if err != nil {
			return response.Err(errors.New("上传任务不存在或已过期"))
		}
		meta = &cache.MultipartMeta{
			UploadID:   in.GetUploadId(),
			UserID:     part.UserID,
			BucketID:   part.BucketID,
			FileName:   part.FileName,
			FileSuffix: part.FileSuffix,
			FileSize:   part.FileSize,
			TotalParts: part.TotalParts,
			StorageID:  in.GetUploadId(),
		}
	}

	// 4. 校验归属
	if meta.UserID != userID {
		return response.Err(errors.New("无权操作该上传任务"))
	}

	// 5. 校验分片完整性
	uploadedCount, err := partRepo.CountUploaded(l.ctx, in.GetUploadId())
	if err != nil {
		return response.Err(errors.New("查询已上传分片数失败: " + err.Error()))
	}
	if uploadedCount != meta.TotalParts {
		return response.Err(fmt.Errorf("分片不完整：已上传 %d / 共 %d", uploadedCount, meta.TotalParts))
	}

	// 目标后缀白名单（meta.FileSuffix 来自 init 请求）
	if !storage.ValidSuffix(meta.FileSuffix) {
		return response.ErrWithCode(response.CodeStoragePathInvalid, "文件后缀非法")
	}

	// 6. 生成目标路径
	fileID := l.svcCtx.Snowflake.MustIdS()
	date := time.Now().Format("20060102")
	targetPath := fmt.Sprintf("%s/%s/%s.%s", userID, date, fileID, meta.FileSuffix)

	// 7. 调用驱动合并分片（数量 + 逐片 MD5 校验，成功后驱动清理临时目录）
	expectedMD5, err := multipartCache.ListParts(l.ctx, in.GetUploadId())
	if err != nil {
		l.Errorf("读取分片 MD5 状态失败（跳过逐片校验）upload_id=%s: %v", in.GetUploadId(), err)
		expectedMD5 = nil
	}
	if err := l.svcCtx.Storage.CompletePart(l.ctx, meta.StorageID, targetPath, meta.TotalParts, expectedMD5); err != nil {
		return response.Err(errors.New("合并分片失败: " + err.Error()))
	}

	// 8. 计算合并后文件的 MD5（重新读取物理文件）
	fileMD5, err := l.computeFileMD5(targetPath)
	if err != nil {
		l.Errorf("计算合并后文件 MD5 失败 path=%s: %v", targetPath, err)
		fileMD5 = ""
	}

	// 大小治理：以驱动 Stat 实测值为记账基准（客户端声明的 FileSize 不可信）
	statInfo, err := l.svcCtx.Storage.Stat(l.ctx, targetPath)
	if err != nil {
		_ = l.svcCtx.Storage.Delete(l.ctx, targetPath)
		return response.Err(errors.New("校验合并文件大小失败: " + err.Error()))
	}
	if meta.FileSize > 0 && statInfo.Size != meta.FileSize {
		_ = l.svcCtx.Storage.Delete(l.ctx, targetPath)
		return response.Err(errors.New(fmt.Sprintf("合并文件大小不符：实测 %d / 声明 %d", statInfo.Size, meta.FileSize)))
	}

	// 9. 秒传检查：若 MD5 命中已有文件，删除刚合并的物理文件，复用旧记录
	if fileMD5 != "" {
		if exist, _ := fileRepo.GetByMD5(l.ctx, userID, fileMD5); exist != nil {
			// 删除刚合并的物理文件
			_ = l.svcCtx.Storage.Delete(l.ctx, targetPath)
			// 回写 Redis 索引
			_ = md5Cache.Set(l.ctx, userID, fileMD5, exist.ID)
			// 更新分片状态
			_ = partRepo.MarkMerged(l.ctx, in.GetUploadId())
			// 清理 Redis
			_ = multipartCache.Clear(l.ctx, in.GetUploadId())
			l.Infof("multipart complete instant hit user=%s md5=%s file=%s", userID, fileMD5, exist.ID)
			return response.Success(&service.CompleteMultipartUploadResponse{
				File:    toFileMeta(exist),
				Instant: true,
			})
		}
	}

	// 10. 计算 content_type
	contentType := mime.TypeByExtension("." + meta.FileSuffix)
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	// 11. 创建文件元数据
	newFile, err := fileRepo.CreateMultipart(
		l.ctx, fileID, userID, meta.BucketID, meta.FileName, targetPath,
		meta.FileSuffix, contentType, fileMD5, statInfo.Size,
	)
	if err != nil {
		// 入库失败，删除已合并的物理文件
		if delErr := l.svcCtx.Storage.Delete(l.ctx, targetPath); delErr != nil {
			l.Errorf("回滚物理文件失败: %v", delErr)
		}
		return response.Err(errors.New("文件入库失败: " + err.Error()))
	}

	// 12. 写 Redis MD5 索引
	if fileMD5 != "" {
		_ = md5Cache.Set(l.ctx, userID, fileMD5, newFile.ID)
	}

	// 13. 扣减桶容量（原子 UPDATE，失败回滚本次合并产物）
	ok, err := bucketRepo.TryIncUsedSize(l.ctx, meta.BucketID, statInfo.Size)
	if err != nil {
		// 扣减失败：同样回滚本次合并产物（物理文件 + DB 记录 + Redis 清理）
		// 分片状态保留 uploaded（merged 是成功语义，失败时标记会留脏状态并卡死重试）
		if delErr := l.svcCtx.Storage.Delete(l.ctx, targetPath); delErr != nil {
			l.Errorf("回滚物理文件失败: %v", delErr)
		}
		if delErr := fileRepo.Delete(l.ctx, newFile.ID); delErr != nil {
			l.Errorf("回滚文件记录失败: %v", delErr)
		}
		if delErr := multipartCache.Clear(l.ctx, in.GetUploadId()); delErr != nil {
			l.Errorf("回滚上传缓存失败: %v", delErr)
		}
		return response.Err(errors.New("桶容量更新失败: " + err.Error()))
	}
	if !ok {
		// 并发下配额被占满：回滚本次合并产物（物理文件 + DB 记录 + Redis 清理）
		// 分片状态保留 uploaded（merged 是成功语义，失败时标记会留脏状态并卡死重试）
		if delErr := l.svcCtx.Storage.Delete(l.ctx, targetPath); delErr != nil {
			l.Errorf("回滚物理文件失败: %v", delErr)
		}
		if delErr := fileRepo.Delete(l.ctx, newFile.ID); delErr != nil {
			l.Errorf("回滚文件记录失败: %v", delErr)
		}
		if delErr := multipartCache.Clear(l.ctx, in.GetUploadId()); delErr != nil {
			l.Errorf("回滚上传缓存失败: %v", delErr)
		}
		return response.ErrWithCode(response.CodeBucketCapacity, "桶容量不足")
	}

	// 13.2 生成缩略图（增强能力，失败不影响主流程）
	if statInfo.Size > 50*1024 {
		BuildThumb(l.ctx, l.svcCtx, newFile.ID, targetPath, meta.FileSuffix)
	}

	// 14. 更新分片状态为 merged
	if err := partRepo.MarkMerged(l.ctx, in.GetUploadId()); err != nil {
		l.Errorf("更新分片状态为 merged 失败 upload_id=%s: %v", in.GetUploadId(), err)
	}

	// 15. 清理 Redis meta/parts
	_ = multipartCache.Clear(l.ctx, in.GetUploadId())

	l.Infof("multipart complete success user=%s upload_id=%s file=%s size=%d md5=%s",
		userID, in.GetUploadId(), newFile.ID, statInfo.Size, fileMD5)

	return response.Success(&service.CompleteMultipartUploadResponse{
		File:    toFileMeta(newFile),
		Instant: false,
	})
}

// computeFileMD5 读取物理文件计算 MD5
func (l *CompleteMultipartUploadLogic) computeFileMD5(path string) (string, error) {
	reader, err := l.svcCtx.Storage.Get(l.ctx, path)
	if err != nil {
		return "", err
	}
	defer reader.Close()
	hash := md5.New()
	if _, err := io.Copy(hash, reader); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
