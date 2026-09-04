package filelogic

import (
	"context"

	"onecloud/proto/.proto/service"
	"onecloud/proto/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

type InitMultipartUploadLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewInitMultipartUploadLogic(ctx context.Context, svcCtx *svc.ServiceContext) *InitMultipartUploadLogic {
	return &InitMultipartUploadLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// ===== 分片上传 =====
func (l *InitMultipartUploadLogic) InitMultipartUpload(in *service.InitMultipartUploadRequest) (*service.Response, error) {
	// todo: add your logic here and delete this line

	return &service.Response{}, nil
}
