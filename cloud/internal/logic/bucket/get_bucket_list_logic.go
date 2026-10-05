package bucketlogic

import (
	"context"
	"errors"

	"onecloud/internal/pkg/jwt"
	"onecloud/internal/pkg/response"
	"onecloud/internal/repository"
	"onecloud/internal/svc"
	"onecloud/proto/.proto/service"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetBucketListLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetBucketListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetBucketListLogic {
	return &GetBucketListLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// GetBucketList 获取当前用户的桶列表
func (l *GetBucketListLogic) GetBucketList(in *service.BucketListRequest) (*service.Response, error) {
	if err := in.Validate(); err != nil {
		return response.Err(errors.New("参数校验失败: " + err.Error()))
	}

	userID, err := jwt.GetUserIdFromCtx(l.ctx)
	if err != nil {
		return response.Err(err)
	}

	bucketRepo := repository.NewBucketRepository(l.svcCtx.EntClient)
	list, total, err := bucketRepo.ListByUser(l.ctx, userID, in.GetPage(), in.GetPageSize())
	if err != nil {
		return response.ErrInternal(l.Logger, "查询桶列表失败", err)
	}

	// 聚合各桶文件数（单条 GROUP BY 查询，避免 N+1）
	countMap, err := bucketRepo.CountFilesGroupByBucket(l.ctx, userID)
	if err != nil {
		l.Errorf("聚合桶文件数失败（列表继续，文件数留空）: %v", err)
		countMap = map[string]int64{}
	}

	buckets := make([]*service.BucketMeta, 0, len(list))
	for _, b := range list {
		meta := toBucketMeta(b)
		if n, ok := countMap[b.ID]; ok {
			meta.FileCount = &n
		}
		buckets = append(buckets, meta)
	}

	return response.Success(&service.BucketListResponse{
		Buckets: buckets,
		Total:   int32(total),
	})
}
