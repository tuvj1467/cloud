package filelogic

import (
	"context"
	"errors"
	"io"
	"time"

	"onecloud/ent"
	"onecloud/internal/pkg/cache"
	"onecloud/internal/pkg/response"
	"onecloud/internal/repository"
	"onecloud/internal/svc"
	"onecloud/proto/.proto/service"

	"github.com/zeromicro/go-zero/core/logx"
)

type DownloadByShareLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewDownloadByShareLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DownloadByShareLogic {
	return &DownloadByShareLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// DownloadByShare 分享下载文件（匿名）
// 步骤：查 Redis 缓存 → 未命中查 DB → 校验过期 → 校验次数
//
//	→ 递增 used_count → 查文件 → 读物理文件
func (l *DownloadByShareLogic) DownloadByShare(in *service.DownloadByShareRequest) (*service.Response, error) {
	// 1. 校验入参
	if err := in.Validate(); err != nil {
		return response.Err(errors.New("参数校验失败: " + err.Error()))
	}

	fileRepo := repository.NewFileRepository(l.svcCtx.EntClient)
	shareRepo := repository.NewFileShareRepository(l.svcCtx.EntClient)
	shareCache := cache.NewShareCache(l.svcCtx.Redis)

	// 2. 优先查 Redis 缓存
	data, err := shareCache.GetMeta(l.ctx, in.GetShareCode())
	if err != nil {
		l.Errorf("查询分享缓存失败: %v", err)
	}
	if data == nil {
		// 3. Redis 未命中，查 DB
		share, err := shareRepo.GetByCode(l.ctx, in.GetShareCode())
		if err != nil {
			if ent.IsNotFound(err) {
				return response.ErrWithCode(response.CodeFileShareNotFound, "分享不存在或已撤销")
			}
			return response.Err(errors.New("查询分享失败: " + err.Error()))
		}
		// 转换为缓存结构
		data = &cache.ShareData{
			ID:         share.ID,
			FileID:     share.FileID,
			UserID:     share.UserID,
			ShareCode:  share.ShareCode,
			Permission: string(share.Permission),
			ExpireAt:   nil,
			MaxCount:   share.MaxCount,
		}
		if share.ExpireAt != nil {
			expireAt := *share.ExpireAt
			data.ExpireAt = &expireAt
		}
		// 回写 Redis
		usedCount, _ := shareCache.GetUsedCount(l.ctx, in.GetShareCode())
		if usedCount == 0 {
			usedCount = share.UsedCount
		}
		_ = shareCache.Set(l.ctx, data, usedCount)
	}

	// 4. 校验过期
	if data.ExpireAt != nil && data.ExpireAt.Before(time.Now()) {
		return response.ErrWithCode(response.CodeFileShareExpired, "分享已过期")
	}

	// 5-6. 校验并递增下载次数（Lua 原子「校验+递增」，消除先查后增的并发突破）
	maxCount := 0
	if data.MaxCount != nil {
		maxCount = int(*data.MaxCount)
	}
	allowed, newCount, err := shareCache.AllowDownload(l.ctx, in.GetShareCode(), maxCount)
	if err != nil {
		l.Errorf("下载计数失败（放行，Redis 兜底对账）: %v", err)
	}
	if !allowed {
		return response.ErrWithCode(response.CodeFileShareExhausted, "分享下载次数已用尽")
	}

	// 7. 同步到 DB（每次下载都同步，保证持久化；version 冲突时放弃本次扣减（仅记录日志），以 Redis 计数为准）
	if newCount > 0 {
		share, err := shareRepo.GetByCode(l.ctx, in.GetShareCode())
		if err == nil {
			if _, err := shareRepo.IncUsedCount(l.ctx, share.ID, share.Version); err != nil {
				l.Errorf("同步下载次数到 DB 失败 share_id=%s: %v", share.ID, err)
			}
		}
	}

	// 8. 查询文件元信息
	f, err := fileRepo.GetInfo(l.ctx, data.FileID)
	if err != nil {
		if ent.IsNotFound(err) {
			return response.Err(errors.New("分享的文件已被删除"))
		}
		return response.Err(errors.New("查询文件失败: " + err.Error()))
	}

	// permission 不限制下载，read 权限下载时留痕
	if data.Permission == "read" {
		l.Infof("share download with read permission code=%s file=%s", in.GetShareCode(), data.FileID)
	}

	// 9. 通过存储驱动读取物理文件
	reader, err := l.svcCtx.Storage.Get(l.ctx, f.Path)
	if err != nil {
		l.Errorf("读取物理文件失败 path=%s: %v", f.Path, err)
		return response.ErrWithCode(response.CodeStorageRead, "文件读取失败")
	}
	defer reader.Close()

	content, err := io.ReadAll(reader)
	if err != nil {
		l.Errorf("读取文件流失败 path=%s: %v", f.Path, err)
		return response.ErrWithCode(response.CodeStorageRead, "文件读取失败")
	}

	l.Infof("download by share success code=%s file=%s size=%d used=%d",
		in.GetShareCode(), f.ID, f.Size, newCount)

	// 审计：记录下载（次数上限仅约束下载，此处已扣减）
	if logRepo := repository.NewShareAccessLogRepository(l.svcCtx.EntClient); logRepo != nil {
		ip, ua := clientInfo(l.ctx)
		if err := logRepo.Create(l.ctx, data.ID, data.ShareCode, "download", ip, ua); err != nil {
			l.Errorf("写入分享下载审计失败 code=%s: %v", data.ShareCode, err)
		}
	}

	return response.Success(&service.DownloadByShareResponse{
		Content: content,
		File:    anonymizeFileMeta(toFileMeta(f)),
	})
}
