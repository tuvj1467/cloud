package filelogic

import (
	"context"
	"errors"

	"onecloud/internal/pkg/jwt"
	"onecloud/internal/pkg/response"
	"onecloud/internal/pkg/snapshot"
	"onecloud/internal/svc"
	"onecloud/proto/.proto/service"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetStorageTrendLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetStorageTrendLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetStorageTrendLogic {
	return &GetStorageTrendLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// GetStorageTrend 用户用量趋势（默认 30 天，快照缺失日补零，当日实时聚合）
func (l *GetStorageTrendLogic) GetStorageTrend(in *service.GetStorageTrendRequest) (*service.Response, error) {
	if err := in.Validate(); err != nil {
		return response.Err(errors.New("参数校验失败"))
	}

	userID, err := jwt.GetUserIdFromCtx(l.ctx)
	if err != nil {
		return response.Err(err)
	}

	days := 30
	if in.Days != nil && *in.Days > 0 {
		days = int(*in.Days)
	}

	points, err := snapshot.Trend(l.ctx, l.svcCtx.EntClient, userID, days)
	if err != nil {
		l.Errorf("查询用量趋势失败 user=%s: %v", userID, err)
		return response.Err(errors.New("查询用量趋势失败"))
	}

	out := make([]*service.TrendPoint, 0, len(points))
	for _, p := range points {
		out = append(out, &service.TrendPoint{
			Date:      p.Date,
			FileCount: p.FileCount,
			TotalSize: p.TotalSize,
		})
	}
	return response.Success(&service.GetStorageTrendResponse{Points: out})
}
