package filelogic

import (
	"context"
	"errors"

	"onecloud/ent"
	"onecloud/internal/pkg/response"
	"onecloud/internal/repository"
	"onecloud/internal/svc"
	"onecloud/proto/.proto/service"

	"github.com/zeromicro/go-zero/core/logx"
)

type AccessShareLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewAccessShareLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AccessShareLogic {
	return &AccessShareLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// AccessShare 访问文件分享（匿名）
// 步骤：加载分享（Redis 优先，DB 兜底，见 loadShareForAccess）→ 查询文件元信息
//
//	→ 返回 FileMeta + permission
//	（次数上限仅约束下载 DownloadByShare/Stream，访问页不计次）
func (l *AccessShareLogic) AccessShare(in *service.AccessShareRequest) (*service.Response, error) {
	// 1. 校验入参
	if err := in.Validate(); err != nil {
		return response.Err(errors.New("参数校验失败: " + err.Error()))
	}

	fileRepo := repository.NewFileRepository(l.svcCtx.EntClient)

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

	// 5. 查询文件元信息
	f, err := fileRepo.GetInfo(l.ctx, data.FileID)
	if err != nil {
		if ent.IsNotFound(err) {
			return response.Err(errors.New("分享的文件已被删除"))
		}
		return response.Err(errors.New("查询文件失败: " + err.Error()))
	}

	l.Infof("access share code=%s file=%s permission=%s",
		in.GetShareCode(), data.FileID, data.Permission)

	// 审计：记录访问（次数上限仅约束下载，access 不扣减）
	if logRepo := repository.NewShareAccessLogRepository(l.svcCtx.EntClient); logRepo != nil {
		ip, ua := clientInfo(l.ctx)
		if err := logRepo.Create(l.ctx, data.ID, data.ShareCode, "access", ip, ua); err != nil {
			l.Errorf("写入分享访问审计失败 code=%s: %v", data.ShareCode, err)
		}
	}

	return response.Success(&service.AccessShareResponse{
		File:       anonymizeFileMeta(toFileMeta(f)),
		Permission: data.Permission,
	})
}
