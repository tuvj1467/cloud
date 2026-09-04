package bucketlogic

import (
	"context"

	"onecloud/proto/.proto/service"
	"onecloud/proto/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetBucketInfoLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetBucketInfoLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetBucketInfoLogic {
	return &GetBucketInfoLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 获取桶详情
func (l *GetBucketInfoLogic) GetBucketInfo(in *service.BucketInfoRequest) (*service.Response, error) {
	// todo: add your logic here and delete this line

	return &service.Response{}, nil
}
