package bucketlogic

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

// GetBucketInfo 获取桶详情
func (l *GetBucketInfoLogic) GetBucketInfo(in *service.BucketInfoRequest) (*service.Response, error) {
	if err := in.Validate(); err != nil {
		return response.Err(errors.New("参数校验失败: " + err.Error()))
	}

	userID, err := jwt.GetUserIdFromCtx(l.ctx)
	if err != nil {
		return response.Err(err)
	}

	bucketRepo := repository.NewBucketRepository(l.svcCtx.EntClient)
	b, err := bucketRepo.GetByID(l.ctx, in.GetBucketId())
	if err != nil {
		if ent.IsNotFound(err) {
			return response.Err(errors.New("桶不存在"))
		}
		return response.Err(errors.New("查询桶失败: " + err.Error()))
	}

	// 校验桶归属
	if b.UserID != userID {
		return response.Err(errors.New("无权访问该桶"))
	}

	return response.Success(&service.BucketInfoResponse{
		Bucket: toBucketMeta(b),
	})
}
