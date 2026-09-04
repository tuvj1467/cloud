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

type UpdateBucketLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewUpdateBucketLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateBucketLogic {
	return &UpdateBucketLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// UpdateBucket 更新桶（描述、容量、权限）
func (l *UpdateBucketLogic) UpdateBucket(in *service.UpdateBucketRequest) (*service.Response, error) {
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
		return response.Err(errors.New("无权修改该桶"))
	}

	// 容量不可小于已用容量（仅当传入 size_limit 时校验）
	if in.SizeLimit != nil && in.GetSizeLimit() < b.UsedSize {
		return response.Err(errors.New("容量限制不能小于已用容量"))
	}

	_, err = bucketRepo.Update(l.ctx, in.GetBucketId(), in)
	if err != nil {
		return response.Err(errors.New("更新桶失败: " + err.Error()))
	}

	return response.Success(&service.UpdateBucketResponse{Success: true})
}
