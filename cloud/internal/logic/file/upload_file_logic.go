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
	"onecloud/internal/pkg/storage"
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

	// 大小治理：以服务端 len(content) 为准，客户端声明不一致直接拒绝
	size := int64(len(in.GetContent()))
	if size == 0 {
		return response.Err(errors.New("文件内容为空"))
	}
	if size != in.GetSize() {
		return response.Err(errors.New("文件大小与内容不一致，请重新上传"))
	}
	if limit := l.svcCtx.Config.Storage.MaxUploadSize; limit > 0 && size > limit {
		return response.ErrWithCode(response.CodeFileTooLarge, "文件超过大小上限")
	}
	// 直传单独限幅：bytes 整文件驻内存（网关读入+反序列化+MD5），与分片通道不同档
	// 默认 5MB（前端 DIRECT_MAX 对齐，大文件前端本就走分片）；≤0 回落 MaxUploadSize
	directLimit := l.svcCtx.Config.Storage.DirectUploadSize
	if directLimit <= 0 {
		directLimit = l.svcCtx.Config.Storage.MaxUploadSize
	}
	if directLimit > 0 && size > directLimit {
		return response.ErrWithCode(response.CodeFileTooLarge,
			"文件超过直传上限，请使用分片上传")
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
		return response.ErrInternal(l.Logger, "查询桶失败", err)
	}
	if b.UserID != userID {
		return response.ErrWithCode(response.CodeBucketNoPermission, "无权操作该桶")
	}

	// 4. 校验桶容量（快速失败，最终以原子扣减为准）
	if b.UsedSize+size > b.SizeLimit {
		return response.ErrWithCode(response.CodeBucketCapacity, "桶容量不足")
	}

	// 5. 计算 MD5
	md5 := util.CalculateFileMD5(in.GetContent())

	// 6. 秒传检查：优先查 Redis 索引，未命中回退 DB
	// 命中要求 size 一致（同 MD5 不同 size 视为碰撞/脏数据，走完整上传，不信任索引）
	md5Cache := cache.NewMD5Cache(l.svcCtx.Redis)
	var instantSrc *ent.File
	if cachedFileID, _ := md5Cache.Get(l.ctx, userID, md5); cachedFileID != "" {
		if exist, _ := fileRepo.GetInfo(l.ctx, cachedFileID); exist != nil && exist.DeletedAt == nil && exist.Size == size {
			instantSrc = exist
		} else {
			// Redis 索引指向的文件已删除或大小不符（脏数据），清理后继续
			_ = md5Cache.Del(l.ctx, userID, md5, cachedFileID)
		}
	}
	if instantSrc == nil {
		// DB 兜底（GetByMD5 已过滤回收站文件），命中后回写 Redis 重建索引
		if exist, _ := fileRepo.GetByMD5(l.ctx, userID, md5); exist != nil && exist.Size == size {
			_ = md5Cache.Set(l.ctx, userID, md5, exist.ID)
			instantSrc = exist
		}
	}
	// 同桶同名直接复用既有记录；跨桶/改名场景在下面共享物理路径新建记录
	//（原实现直接返回旧记录：目标桶文件列表看不到该文件、目标桶配额未计，语义错误）
	if instantSrc != nil && instantSrc.BucketID == in.GetBucketId() && instantSrc.Name == in.GetName() {
		l.Infof("instant hit user=%s md5=%s file=%s", userID, md5, instantSrc.ID)
		return response.Success(&service.UploadFileResponse{
			File:    toFileMeta(instantSrc),
			Instant: true,
		})
	}

	// 7. 生成文件ID与存储路径
	// 后缀白名单（防路径穿越：suffix 曾可携带 ../ 构造越界写入）
	if !storage.ValidSuffix(in.GetSuffix()) {
		return response.ErrWithCode(response.CodeStoragePathInvalid, "文件后缀非法")
	}
	fileID := l.svcCtx.Snowflake.MustIdS()
	filePath := instantSrc.Path // 秒传跨桶/改名：共享既有物理文件，不再写物理
	if instantSrc == nil {
		date := time.Now().Format("20060102")
		filePath = fmt.Sprintf("%s/%s/%s.%s", userID, date, fileID, in.GetSuffix())
	}
	contentType := mime.TypeByExtension("." + in.GetSuffix())
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	// 8. 先原子扣减桶容量（配额优先：后续任一步失败只需补偿性归还，
	// 不存在「记录已建但配额未扣」的崩溃漂移窗口）
	ok, err := bucketRepo.TryIncUsedSize(l.ctx, in.GetBucketId(), size)
	if err != nil {
		return response.ErrInternal(l.Logger, "桶容量更新失败", err)
	}
	if !ok {
		return response.ErrWithCode(response.CodeBucketCapacity, "桶容量不足")
	}

	// rollback 失败补偿：归还配额 + 清理本次产物（新建物理文件仅存在于完整上传分支）
	rollback := func(freshPath string, recordID string) {
		if recordID != "" {
			if delErr := fileRepo.Delete(l.ctx, recordID); delErr != nil {
				l.Errorf("回滚文件记录失败: %v", delErr)
			}
			if delErr := md5Cache.Del(l.ctx, userID, md5, recordID); delErr != nil {
				l.Errorf("回滚 MD5 索引失败: %v", delErr)
			}
		}
		if freshPath != "" {
			if delErr := l.svcCtx.Storage.Delete(l.ctx, freshPath); delErr != nil {
				l.Errorf("回滚物理文件失败: %v", delErr)
			}
		}
		if _, decErr := bucketRepo.TryDecUsedSize(l.ctx, in.GetBucketId(), size); decErr != nil {
			// 配额泄漏是最坏结果：持久化告警等待人工/ GC 对账
			l.Errorf("回滚归还桶容量失败（配额可能泄漏，需对账）bucket=%s delta=%d: %v", in.GetBucketId(), size, decErr)
		}
	}

	// 9. 写入物理文件（秒传跳过——共享既有物理路径）
	if instantSrc == nil {
		if err := l.svcCtx.Storage.Put(l.ctx, filePath, bytes.NewReader(in.GetContent()), size); err != nil {
			rollback("", "")
			return response.ErrInternal(l.Logger, "文件存储失败", err)
		}
	}

	// 10. 写入文件元信息
	res, err := fileRepo.Create(l.ctx, in, fileID, userID, in.GetBucketId(), filePath, contentType, md5)
	if err != nil {
		rollback(freshPathOf(instantSrc, filePath), "")
		return response.ErrInternal(l.Logger, "文件入库失败", err)
	}

	// 10.1 写入 Redis MD5 索引，便于后续秒传
	if err := md5Cache.Set(l.ctx, userID, md5, res.ID); err != nil {
		l.Errorf("写入 MD5 索引失败（不影响主流程）: %v", err)
	}

	// 10.2 生成缩略图（增强能力，失败不影响主流程）
	if size > ThumbMinSize {
		BuildThumbAsync(l.ctx, l.svcCtx, res.ID, filePath, in.GetSuffix())
	}

	l.Infof("upload success user=%s bucket=%s file=%s size=%d md5=%s instant_new_record=%v",
		userID, in.GetBucketId(), res.ID, size, md5, instantSrc != nil)

	return response.Success(&service.UploadFileResponse{
		File:    toFileMeta(res),
		Instant: instantSrc != nil,
	})
}

// freshPathOf 完整上传的物理文件是新建的（回滚需删除）；秒传共享的是既有文件（不可删）
func freshPathOf(instantSrc *ent.File, filePath string) string {
	if instantSrc != nil {
		return ""
	}
	return filePath
}
