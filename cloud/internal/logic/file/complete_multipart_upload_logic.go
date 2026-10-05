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

	"onecloud/ent"
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
// 步骤：校验 upload_id → 校验分片完整性 → 原子占有合并权（uploaded→merged 一次性状态迁移）
//
//	→ 生成目标路径 → 调用驱动 CompletePart 合并 → 计算合并后 MD5 → 秒传检查
//	→ 创建 File 元数据 → 更新桶容量 → 写 Redis MD5 索引 → 清理 Redis meta/parts
//
// 合并权门闩：并发两次 Complete 同一 upload_id 时，仅一次能迁移成功，
// 杜绝重复合并物理文件 / 重复建记录 / 重复扣配额；失败路径恢复 uploaded 保留重试能力
// （进程在门闩与回滚之间崩溃会留下 merged 僵尸记录，该 upload_id 需重新初始化——
// 以极小概率的数据残留换取配额不被重复扣减）
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
		return response.ErrInternal(l.Logger, "查询上传元信息失败", err)
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
		return response.ErrInternal(l.Logger, "查询已上传分片数失败", err)
	}
	if uploadedCount != meta.TotalParts {
		return response.Err(fmt.Errorf("分片不完整：已上传 %d / 共 %d", uploadedCount, meta.TotalParts))
	}

	// 5.1 原子占有合并权（uploaded → merged 一次性迁移）
	owned, err := partRepo.TryMarkMerging(l.ctx, in.GetUploadId(), meta.TotalParts)
	if err != nil {
		return response.ErrInternal(l.Logger, "占有合并权失败", err)
	}
	if !owned {
		return response.Err(errors.New("该上传任务正在合并或已完成，请勿重复提交"))
	}

	// rollback 合并失败回滚：清理本次产物并把分片状态恢复为 uploaded（保留重试能力）
	// restoreQuota：配额在本流程第 11 步先扣，扣减之后的失败路径须补偿性归还。
	// mergedSize 在 Stat 实测后赋值，早于所有 restoreQuota=true 的调用点
	var mergedSize int64
	rollback := func(physicalPath string, recordID string, restoreQuota bool) {
		if restoreQuota {
			if _, decErr := bucketRepo.TryDecUsedSize(l.ctx, meta.BucketID, mergedSize); decErr != nil {
				l.Errorf("回滚归还桶容量失败（配额可能泄漏，需对账）bucket=%s delta=%d: %v", meta.BucketID, mergedSize, decErr)
			}
		}
		if physicalPath != "" {
			if delErr := l.svcCtx.Storage.Delete(l.ctx, physicalPath); delErr != nil {
				l.Errorf("回滚物理文件失败: %v", delErr)
			}
		}
		if recordID != "" {
			if delErr := fileRepo.Delete(l.ctx, recordID); delErr != nil {
				l.Errorf("回滚文件记录失败: %v", delErr)
			}
		}
		if delErr := multipartCache.Clear(l.ctx, in.GetUploadId()); delErr != nil {
			l.Errorf("回滚上传缓存失败: %v", delErr)
		}
		if delErr := partRepo.RestoreUploaded(l.ctx, in.GetUploadId()); delErr != nil {
			l.Errorf("恢复分片状态失败（该 upload_id 可能需要重新初始化）: %v", delErr)
		}
	}

	// 目标后缀白名单（meta.FileSuffix 来自 init 请求）
	if !storage.ValidSuffix(meta.FileSuffix) {
		rollback("", "", false)
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
		rollback("", "", false)
		return response.ErrInternal(l.Logger, "合并分片失败", err)
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
		rollback(targetPath, "", false)
		return response.ErrInternal(l.Logger, "校验合并文件大小失败", err)
	}
	mergedSize = statInfo.Size
	if meta.FileSize > 0 && statInfo.Size != meta.FileSize {
		rollback(targetPath, "", false)
		return response.Err(fmt.Errorf("合并文件大小不符：实测 %d / 声明 %d", statInfo.Size, meta.FileSize))
	}

	// 9. 秒传检查：MD5 且 size 一致才命中（同 MD5 不同 size 视为碰撞，不复用）
	var instantSrc *ent.File
	if fileMD5 != "" {
		if exist, _ := fileRepo.GetByMD5(l.ctx, userID, fileMD5); exist != nil && exist.Size == statInfo.Size {
			instantSrc = exist
		}
	}
	// 同桶同名：删除刚合并的物理文件，直接复用旧记录（原语义）
	if instantSrc != nil && instantSrc.BucketID == meta.BucketID && instantSrc.Name == meta.FileName {
		_ = l.svcCtx.Storage.Delete(l.ctx, targetPath)
		_ = md5Cache.Set(l.ctx, userID, fileMD5, instantSrc.ID)
		_ = multipartCache.Clear(l.ctx, in.GetUploadId())
		l.Infof("multipart complete instant hit user=%s md5=%s file=%s", userID, fileMD5, instantSrc.ID)
		return response.Success(&service.CompleteMultipartUploadResponse{
			File:    toFileMeta(instantSrc),
			Instant: true,
		})
	}
	// 跨桶/改名秒传：共享既有物理路径新建记录（刚合并的重复物理文件删除），仍计入目标桶配额
	recordPath := targetPath
	if instantSrc != nil {
		_ = l.svcCtx.Storage.Delete(l.ctx, targetPath)
		recordPath = instantSrc.Path
	}

	// 10. 计算 content_type
	contentType := mime.TypeByExtension("." + meta.FileSuffix)
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	// 11. 先原子扣减桶容量（配额优先：后续失败只需补偿归还，杜绝「记录已建配额未扣」的漂移窗口）
	ok, err := bucketRepo.TryIncUsedSize(l.ctx, meta.BucketID, statInfo.Size)
	if err != nil || !ok {
		rollback(freshPathOf(instantSrc, targetPath), "", false)
		if err != nil {
			return response.ErrInternal(l.Logger, "桶容量更新失败", err)
		}
		return response.ErrWithCode(response.CodeBucketCapacity, "桶容量不足")
	}

	// 12. 创建文件元数据
	newFile, err := fileRepo.CreateMultipart(
		l.ctx, fileID, userID, meta.BucketID, meta.FileName, recordPath,
		meta.FileSuffix, contentType, fileMD5, statInfo.Size,
	)
	if err != nil {
		rollback(freshPathOf(instantSrc, targetPath), "", true)
		return response.ErrInternal(l.Logger, "文件入库失败", err)
	}

	// 13. 写 Redis MD5 索引
	if fileMD5 != "" {
		_ = md5Cache.Set(l.ctx, userID, fileMD5, newFile.ID)
	}

	// 13.2 生成缩略图（增强能力，失败不影响主流程）
	if statInfo.Size > ThumbMinSize {
		BuildThumbAsync(l.ctx, l.svcCtx, newFile.ID, recordPath, meta.FileSuffix)
	}

	// 14. 清理 Redis meta/parts（分片状态已在门闩处置为 merged）
	_ = multipartCache.Clear(l.ctx, in.GetUploadId())

	l.Infof("multipart complete success user=%s upload_id=%s file=%s size=%d md5=%s instant_new_record=%v",
		userID, in.GetUploadId(), newFile.ID, statInfo.Size, fileMD5, instantSrc != nil)

	return response.Success(&service.CompleteMultipartUploadResponse{
		File:    toFileMeta(newFile),
		Instant: instantSrc != nil,
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
