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
		return response.Err(errors.New("查询桶列表失败: " + err.Error()))
	}

	buckets := make([]*service.BucketMeta, 0, len(list))
	for _, b := range list {
		buckets = append(buckets, toBucketMeta(b))
	}

	return response.Success(&service.BucketListResponse{
		Buckets: buckets,
		Total:   int32(total),
	})
}
