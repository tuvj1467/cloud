package filelogic

import (
	"context"
	"errors"
	"fmt"
	"time"

	"onecloud/ent"
	"onecloud/internal/pkg/cache"
	"onecloud/internal/repository"
	"onecloud/internal/svc"
)

// 匿名分享链路公共哨兵错误（调用方映射为对应错误码/文案）
var (
	// ErrShareNotFound 分享不存在或已撤销
	ErrShareNotFound = errors.New("分享不存在或已撤销")
	// ErrShareExpired 分享已过期
	ErrShareExpired = errors.New("分享已过期")
)

// loadShareForAccess 匿名分享访问公共流程：Redis 优先加载分享元信息
// （未命中查 DB 并回写缓存），校验过期。access / download / download_stream
// 三个入口共用，避免缓存回填逻辑三处漂移
func loadShareForAccess(ctx context.Context, svcCtx *svc.ServiceContext, shareCode string) (*cache.ShareData, error) {
	shareRepo := repository.NewFileShareRepository(svcCtx.EntClient)
	shareCache := cache.NewShareCache(svcCtx.Redis)

	data, err := shareCache.GetMeta(ctx, shareCode)
	if err != nil {
		// 缓存故障降级直查 DB（fail-open 读取）
		data = nil
	}
	if data == nil {
		share, err := shareRepo.GetByCode(ctx, shareCode)
		if err != nil {
			if ent.IsNotFound(err) {
				return nil, ErrShareNotFound
			}
			return nil, fmt.Errorf("查询分享失败: %w", err)
		}
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
		// 回写 Redis（已用次数优先取 Redis 计数，兜底 DB 值）
		usedCount, _ := shareCache.GetUsedCount(ctx, shareCode)
		if usedCount == 0 {
			usedCount = share.UsedCount
		}
		_ = shareCache.Set(ctx, data, usedCount)
	}

	if data.ExpireAt != nil && data.ExpireAt.Before(time.Now()) {
		return nil, ErrShareExpired
	}
	return data, nil
}
