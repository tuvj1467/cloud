package filelogic

import (
	"context"
	"errors"

	"onecloud/ent"
	"onecloud/ent/file"
	"onecloud/internal/pkg/cache"
	"onecloud/internal/pkg/jwt"
	"onecloud/internal/pkg/response"
	"onecloud/internal/repository"
	"onecloud/internal/svc"
	"onecloud/proto/.proto/service"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetMySharesLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetMySharesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetMySharesLogic {
	return &GetMySharesLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// GetMyShares 我的分享列表（分页，含文件名冗余）
func (l *GetMySharesLogic) GetMyShares(in *service.GetMySharesRequest) (*service.Response, error) {
	if err := in.Validate(); err != nil {
		return response.Err(errors.New("参数校验失败"))
	}

	userID, err := jwt.GetUserIdFromCtx(l.ctx)
	if err != nil {
		return response.Err(err)
	}

	shareRepo := repository.NewFileShareRepository(l.svcCtx.EntClient)
	shares, total, err := shareRepo.ListByUser(l.ctx, userID, int(in.GetPage()), int(in.GetPageSize()))
	if err != nil {
		return response.Err(errors.New("查询分享列表失败: " + err.Error()))
	}

	// 批量取文件名（一次 IN 查询）
	fileIDs := make([]string, 0, len(shares))
	for _, s := range shares {
		fileIDs = append(fileIDs, s.FileID)
	}
	nameMap := map[string]string{}
	if len(fileIDs) > 0 {
		files, err := l.svcCtx.EntClient.File.Query().Where(file.IDIn(fileIDs...)).All(l.ctx)
		if err == nil {
			for _, f := range files {
				nameMap[f.ID] = f.Name
			}
		} else {
			l.Errorf("批量查询分享文件名失败: %v", err)
		}
	}

	items := make([]*service.ShareItem, 0, len(shares))
	for _, s := range shares {
		item := &service.ShareItem{
			ShareCode:  s.ShareCode,
			FileId:     s.FileID,
			FileName:   nameMap[s.FileID],
			Permission: string(s.Permission),
			UsedCount:  int64(s.UsedCount),
			CreatedAt:  s.CreatedAt.Format("2006-01-02 15:04:05"),
		}
		if s.ExpireAt != nil {
			item.ExpireAt = s.ExpireAt.Format("2006-01-02 15:04:05")
		}
		if s.MaxCount != nil {
			mc := int32(*s.MaxCount)
			item.MaxCount = &mc
		}
		items = append(items, item)
	}

	return response.Success(&service.GetMySharesResponse{Shares: items, Total: int32(total)})
}

// RevokeShare 撤销分享：DB 记录删除 + Redis 缓存清理（仅限本人分享）
func (l *GetMySharesLogic) RevokeShare(in *service.RevokeShareRequest) (*service.Response, error) {
	if err := in.Validate(); err != nil {
		return response.Err(errors.New("参数校验失败"))
	}

	userID, err := jwt.GetUserIdFromCtx(l.ctx)
	if err != nil {
		return response.Err(err)
	}

	shareRepo := repository.NewFileShareRepository(l.svcCtx.EntClient)
	s, err := shareRepo.GetByCode(l.ctx, in.GetShareCode())
	if err != nil {
		if ent.IsNotFound(err) {
			return response.ErrWithCode(response.CodeFileShareNotFound, "分享不存在")
		}
		return response.Err(errors.New("查询分享失败: " + err.Error()))
	}
	if s.UserID != userID {
		return response.ErrWithCode(response.CodeFileNoPermission, "只能撤销自己的分享")
	}

	if err := shareRepo.DeleteByID(l.ctx, s.ID); err != nil {
		return response.Err(errors.New("撤销分享失败: " + err.Error()))
	}
	// Redis 缓存清理（分享 meta / 计数）——经 nil 安全的 ShareCache，禁止直连 svcCtx.Redis
	shareCache := cache.NewShareCache(l.svcCtx.Redis)
	if err := shareCache.Del(l.ctx, s.ShareCode); err != nil {
		l.Errorf("清理分享缓存失败 code=%s: %v", s.ShareCode, err)
	}

	l.Infof("share revoked user=%s share_code=%s", userID, s.ShareCode)
	return response.Success(&service.RevokeShareResponse{Success: true})
}
