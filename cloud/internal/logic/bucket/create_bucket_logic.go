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

type CreateBucketLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCreateBucketLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateBucketLogic {
	return &CreateBucketLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// CreateBucket 创建桶
func (l *CreateBucketLogic) CreateBucket(in *service.CreateBucketRequest) (*service.Response, error) {
	// 校验入参
	if err := in.Validate(); err != nil {
		return response.Err(errors.New("参数校验失败: " + err.Error()))
	}

	// 获取当前用户
	userID, err := jwt.GetUserIdFromCtx(l.ctx)
	if err != nil {
		return response.Err(err)
	}

	bucketRepo := repository.NewBucketRepository(l.svcCtx.EntClient)

	// 检查桶名是否重复（同一用户内唯一）
	exist, err := bucketRepo.GetByUserAndName(l.ctx, userID, in.GetName())
	if err != nil && !ent.IsNotFound(err) {
		return response.ErrInternal(l.Logger, "查询桶失败", err)
	}
	if exist != nil {
		return response.Err(errors.New("桶名已存在"))
	}

	// 生成桶ID
	bucketID := l.svcCtx.Snowflake.MustIdS()

	// 创建桶
	b, err := bucketRepo.Create(l.ctx, bucketID, userID, in)
	if err != nil {
		return response.ErrInternal(l.Logger, "创建桶失败", err)
	}

	return response.Success(&service.CreateBucketResponse{
		Bucket: toBucketMeta(b),
	})
}
