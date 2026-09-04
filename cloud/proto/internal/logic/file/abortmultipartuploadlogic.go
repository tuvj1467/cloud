package filelogic

import (
	"context"

	"onecloud/proto/.proto/service"
	"onecloud/proto/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

type AbortMultipartUploadLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewAbortMultipartUploadLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AbortMultipartUploadLogic {
	return &AbortMultipartUploadLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 取消分片上传
func (l *AbortMultipartUploadLogic) AbortMultipartUpload(in *service.AbortMultipartUploadRequest) (*service.Response, error) {
	// todo: add your logic here and delete this line

	return &service.Response{}, nil
}
