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

type DeleteBucketLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewDeleteBucketLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteBucketLogic {
	return &DeleteBucketLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// DeleteBucket 删除桶（非 force 模式下桶必须为空）
func (l *DeleteBucketLogic) DeleteBucket(in *service.DeleteBucketRequest) (*service.Response, error) {
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
		return response.Err(errors.New("无权删除该桶"))
	}

	// 检查桶是否为空
	fileCount, err := bucketRepo.CountFiles(l.ctx, in.GetBucketId())
	if err != nil {
		return response.Err(errors.New("统计桶文件失败: " + err.Error()))
	}
	if fileCount > 0 && !in.GetForce() {
		return response.Err(errors.New("桶非空，无法删除（可使用 force 强制删除）"))
	}

	// TODO: force 模式下批量删除桶内文件（物理文件 + DB 记录），P6 阶段完善

	// 删除桶
	if err := bucketRepo.Delete(l.ctx, in.GetBucketId()); err != nil {
		return response.Err(errors.New("删除桶失败: " + err.Error()))
	}

	return response.Success(&service.DeleteBucketResponse{Success: true})
}
