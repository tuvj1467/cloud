package filelogic

import (
	"context"

	"onecloud/proto/.proto/service"
	"onecloud/proto/internal/svc"

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

// 获取用户对应桶文件列表
func (l *GetBucketFileListLogic) GetBucketFileList(in *service.BucketFileListRequest) (*service.Response, error) {
	// todo: add your logic here and delete this line

	return &service.Response{}, nil
}
