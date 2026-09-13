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

type AdminRecentOpLogsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewAdminRecentOpLogsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AdminRecentOpLogsLogic {
	return &AdminRecentOpLogsLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

// AdminRecentOpLogs 全局最近管理操作审计（admin 专用）
func (l *AdminRecentOpLogsLogic) AdminRecentOpLogs(in *service.AdminRecentOpLogsRequest) (*service.Response, error) {
	if r, ok := AdminGuard(l.ctx); !ok {
		return r, nil
	}

	limit := int(in.GetLimit())
	if limit <= 0 {
		limit = 50
	}

	logRepo := repository.NewAdminAuditLogRepository(l.svcCtx.EntClient)
	logs, err := logRepo.ListRecent(l.ctx, limit)
	if err != nil {
		return response.Err(errors.New("查询管理审计失败"))
	}

	items := make([]*service.AdminOpLogItem, 0, len(logs))
	for _, lg := range logs {
		items = append(items, &service.AdminOpLogItem{
			AdminId:   lg.AdminID,
			Action:    lg.Action,
			TargetId:  lg.TargetID,
			Detail:    lg.Detail,
			CreatedAt: lg.CreatedAt.Format("2006-01-02 15:04:05"),
		})
	}
	return response.Success(&service.AdminRecentOpLogsResponse{Logs: items})
}
