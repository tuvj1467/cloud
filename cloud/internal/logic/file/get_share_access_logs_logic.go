package filelogic

import (
	"context"
	"errors"

	"onecloud/ent"
	"onecloud/internal/pkg/jwt"
	"onecloud/internal/pkg/response"
	"onecloud/internal/repository"
	"onecloud/internal/svc"
	"onecloud/proto/.proto/service"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetShareAccessLogsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetShareAccessLogsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetShareAccessLogsLogic {
	return &GetShareAccessLogsLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

// GetShareAccessLogs 查询分享访问审计日志（分页，时间倒序；仅限本人分享）
func (l *GetShareAccessLogsLogic) GetShareAccessLogs(in *service.GetShareAccessLogsRequest) (*service.Response, error) {
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
		return response.ErrWithCode(response.CodeFileNoPermission, "只能查看自己分享的审计日志")
	}

	logRepo := repository.NewShareAccessLogRepository(l.svcCtx.EntClient)
	logs, total, err := logRepo.ListByShareCode(l.ctx, s.ShareCode, int(in.GetPage()), int(in.GetPageSize()))
	if err != nil {
		return response.Err(errors.New("查询审计日志失败: " + err.Error()))
	}

	items := make([]*service.ShareAccessLogItem, 0, len(logs))
	for _, lg := range logs {
		items = append(items, &service.ShareAccessLogItem{
			Action:    string(lg.Action),
			Ip:        lg.IP,
			UserAgent: lg.UserAgent,
			CreatedAt: lg.CreatedAt.Format("2006-01-02 15:04:05"),
		})
	}
	return response.Success(&service.GetShareAccessLogsResponse{Logs: items, Total: int32(total)})
}
