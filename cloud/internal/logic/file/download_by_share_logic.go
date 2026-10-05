package filelogic

import (
	"context"
	"errors"
	"io"

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
// 步骤：加载分享（Redis 优先，DB 兜底，见 loadShareForAccess）→ 校验过期 → 校验次数
//
//	→ 递增 used_count → 查文件 → 读物理文件
//	（大文件请走流式端点 DownloadByShareStream，本接口整文件驻内存）
func (l *DownloadByShareLogic) DownloadByShare(in *service.DownloadByShareRequest) (*service.Response, error) {
	// 1. 校验入参
	if err := in.Validate(); err != nil {
		return response.Err(errors.New("参数校验失败: " + err.Error()))
	}

	fileRepo := repository.NewFileRepository(l.svcCtx.EntClient)
	shareRepo := repository.NewFileShareRepository(l.svcCtx.EntClient)
	shareCache := cache.NewShareCache(l.svcCtx.Redis)

	// 2-4. 加载分享元信息并校验过期（公共流程）
	data, err := loadShareForAccess(l.ctx, l.svcCtx, in.GetShareCode())
	if err != nil {
		if errors.Is(err, ErrShareNotFound) {
			return response.ErrWithCode(response.CodeFileShareNotFound, err.Error())
		}
		if errors.Is(err, ErrShareExpired) {
			return response.ErrWithCode(response.CodeFileShareExpired, err.Error())
		}
		return response.Err(errors.New("查询分享失败"))
	}

	// 5-6. 校验并递增下载次数（Lua 原子「校验+递增+TTL 续期」）
	allowed, newCount, err := shareCache.AllowDownload(l.ctx, data)
	if err != nil {
		l.Errorf("下载计数失败（放行，Redis 兜底对账）: %v", err)
	}
	if !allowed {
		return response.ErrWithCode(response.CodeFileShareExhausted, "分享下载次数已用尽")
	}

	// 7. 同步到 DB（原子自增；以 Redis 计数为对外口径，DB 仅持久化对账）
	if newCount > 0 {
		if _, err := shareRepo.IncUsedCount(l.ctx, data.ID); err != nil {
			l.Errorf("同步下载次数到 DB 失败 share_id=%s: %v", data.ID, err)
		}
	}

	// permission=read 为仅查看分享（用户创建时显式选择），不允许下载
	if data.Permission == "read" {
		return response.ErrWithCode(response.CodeFileNoPermission, "该分享仅允许查看，不支持下载")
	}

	// 8. 查询文件元信息（回收站中的文件不可被分享下载）
	f, err := fileRepo.GetInfo(l.ctx, data.FileID)
	if err != nil {
		if ent.IsNotFound(err) {
			return response.Err(errors.New("分享的文件已被删除"))
		}
		return response.Err(errors.New("查询文件失败"))
	}
	if f.DeletedAt != nil {
		return response.Err(errors.New("分享的文件已被删除"))
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
