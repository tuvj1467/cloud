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

type AdminGetGlobalStatsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewAdminGetGlobalStatsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AdminGetGlobalStatsLogic {
	return &AdminGetGlobalStatsLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

// AdminGetGlobalStats 全局统计：用户数/文件数/总容量
func (l *AdminGetGlobalStatsLogic) AdminGetGlobalStats(in *service.AdminGlobalStatsRequest) (*service.Response, error) {
	if r, ok := AdminGuard(l.ctx); !ok {
		return r, nil
	}

	users, files, totalSize, err := repository.AdminGlobalCounts(l.svcCtx.EntClient, l.ctx)
	if err != nil {
		return response.Err(errors.New("查询全局统计失败: " + err.Error()))
	}
	return response.Success(&service.AdminGlobalStatsResponse{
		UserCount: int64(users),
		FileCount: int64(files),
		TotalSize: totalSize,
	})
}
