package userlogic

import (
	"context"
	"errors"

	"onecloud/internal/pkg/response"
	"onecloud/internal/repository"
	"onecloud/internal/svc"
	"onecloud/proto/.proto/service"

	"github.com/zeromicro/go-zero/core/logx"
)

type AdminRecentAccessLogsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewAdminRecentAccessLogsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AdminRecentAccessLogsLogic {
	return &AdminRecentAccessLogsLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

// AdminRecentAccessLogs 全局最近分享访问/下载审计（admin 专用，跨用户聚合）
func (l *AdminRecentAccessLogsLogic) AdminRecentAccessLogs(in *service.AdminRecentAccessLogsRequest) (*service.Response, error) {
	if r, ok := AdminGuard(l.ctx); !ok {
		return r, nil
	}

	limit := int(in.GetLimit())
	if limit <= 0 {
		limit = 50
	}

	logRepo := repository.NewShareAccessLogRepository(l.svcCtx.EntClient)
	logs, err := logRepo.ListRecent(l.ctx, limit)
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
			ShareCode: lg.ShareCode,
		})
	}
	return response.Success(&service.AdminRecentAccessLogsResponse{Logs: items})
}
