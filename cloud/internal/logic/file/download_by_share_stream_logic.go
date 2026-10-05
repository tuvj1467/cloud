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

// DownloadByShareStreamLogic 匿名分享流式下载（64KB 分块 server-streaming）
type DownloadByShareStreamLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewDownloadByShareStreamLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DownloadByShareStreamLogic {
	return &DownloadByShareStreamLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// DownloadByShareStream 匿名分享流式下载：校验/计数语义与 DownloadByShare 一致，
// 物理文件按 64KB 分块推送，避免大文件整文件驻内存（匿名接口不可限登录态，
// 内存放大最危险，优先流式化）
func (l *DownloadByShareStreamLogic) DownloadByShareStream(in *service.DownloadByShareRequest, stream service.File_DownloadByShareStreamServer) error {
	if err := in.Validate(); err != nil {
		return errors.New("参数校验失败")
	}

	data, err := loadShareForAccess(stream.Context(), l.svcCtx, in.GetShareCode())
	if err != nil {
		if errors.Is(err, ErrShareNotFound) {
			r, _ := response.ErrWithCode(response.CodeFileShareNotFound, err.Error())
			return errors.New(r.Message)
		}
		if errors.Is(err, ErrShareExpired) {
			r, _ := response.ErrWithCode(response.CodeFileShareExpired, err.Error())
			return errors.New(r.Message)
		}
		return errors.New("查询分享失败")
	}

	shareCache := cache.NewShareCache(l.svcCtx.Redis)
	shareRepo := repository.NewFileShareRepository(l.svcCtx.EntClient)
	fileRepo := repository.NewFileRepository(l.svcCtx.EntClient)

	// 校验并递增下载次数（Lua 原子「校验+递增+TTL 续期」）
	allowed, newCount, err := shareCache.AllowDownload(l.ctx, data)
	if err != nil {
		l.Errorf("下载计数失败（放行，Redis 兜底对账）: %v", err)
	}
	if !allowed {
		r, _ := response.ErrWithCode(response.CodeFileShareExhausted, "分享下载次数已用尽")
		return errors.New(r.Message)
	}

	// 同步到 DB（原子自增，复用缓存中的 share.ID）
	if newCount > 0 {
		if _, err := shareRepo.IncUsedCount(l.ctx, data.ID); err != nil {
			l.Errorf("同步下载次数到 DB 失败 share_id=%s: %v", data.ID, err)
		}
	}

	// permission=read 为仅查看分享（用户创建时显式选择），不允许下载
	if data.Permission == "read" {
		r, _ := response.ErrWithCode(response.CodeFileNoPermission, "该分享仅允许查看，不支持下载")
		return errors.New(r.Message)
	}

	f, err := fileRepo.GetInfo(l.ctx, data.FileID)
	if err != nil {
		if ent.IsNotFound(err) {
			return errors.New("分享的文件已被删除")
		}
		return errors.New("查询文件失败")
	}
	// 回收站中的文件不可被分享下载
	if f.DeletedAt != nil {
		return errors.New("分享的文件已被删除")
	}

	reader, err := l.svcCtx.Storage.Get(l.ctx, f.Path)
	if err != nil {
		l.Errorf("读取物理文件失败 path=%s: %v", f.Path, err)
		return errors.New("文件读取失败")
	}
	defer reader.Close()

	total := f.Size
	if info, err := l.svcCtx.Storage.Stat(l.ctx, f.Path); err == nil && info.Size > 0 {
		total = info.Size
	}

	buf := make([]byte, downloadChunkSize)
	var sent int64
	for {
		n, rErr := reader.Read(buf)
		if n > 0 {
			sent += int64(n)
			if sendErr := stream.Send(&service.DownloadChunk{
				Data:  buf[:n],
				Sent:  sent,
				Total: total,
			}); sendErr != nil {
				return sendErr
			}
		}
		if rErr == io.EOF {
			break
		}
		if rErr != nil {
			l.Errorf("读取文件流失败 path=%s: %v", f.Path, rErr)
			return errors.New("文件读取失败")
		}
	}

	l.Infof("stream download by share success code=%s file=%s size=%d used=%d",
		in.GetShareCode(), f.ID, sent, newCount)

	// 审计：记录下载（次数已扣减）
	if logRepo := repository.NewShareAccessLogRepository(l.svcCtx.EntClient); logRepo != nil {
		ip, ua := clientInfo(l.ctx)
		if err := logRepo.Create(l.ctx, data.ID, data.ShareCode, "download", ip, ua); err != nil {
			l.Errorf("写入分享下载审计失败 code=%s: %v", data.ShareCode, err)
		}
	}
	return nil
}
