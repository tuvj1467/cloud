package filelogic

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

type GetBucketFileListLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetBucketFileListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetBucketFileListLogic {
	return &GetBucketFileListLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// GetBucketFileList 获取用户对应桶的文件列表
func (l *GetBucketFileListLogic) GetBucketFileList(in *service.BucketFileListRequest) (*service.Response, error) {
	if err := in.Validate(); err != nil {
		return response.Err(errors.New("参数校验失败"))
	}

	userID, err := jwt.GetUserIdFromCtx(l.ctx)
	if err != nil {
		return response.Err(err)
	}

	bucketRepo := repository.NewBucketRepository(l.svcCtx.EntClient)
	fileRepo := repository.NewFileRepository(l.svcCtx.EntClient)

	// 校验桶归属
	b, err := bucketRepo.GetByID(l.ctx, in.GetBucketId())
	if err != nil {
		if ent.IsNotFound(err) {
			return response.Err(errors.New("桶不存在"))
		}
		return response.Err(errors.New("查询桶失败: " + err.Error()))
	}
	if b.UserID != userID {
		return response.Err(errors.New("无权访问该桶"))
	}

	// 分页查询桶内文件
	list, total, err := fileRepo.GetBucketFileList(l.ctx, userID, in.GetBucketId(), in)
	if err != nil {
		return response.Err(errors.New("查询桶文件列表失败: " + err.Error()))
	}

	files := make([]*service.FileMeta, 0, len(list))
	for _, f := range list {
		files = append(files, toFileMeta(f))
	}

	return response.Success(&service.FileListResponse{
		Files: files,
		Total: int32(total),
	})
}
