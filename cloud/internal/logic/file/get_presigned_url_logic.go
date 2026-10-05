package filelogic

import (
	"context"
	"errors"
	"time"

	"onecloud/ent"
	"onecloud/internal/pkg/jwt"
	"onecloud/internal/pkg/response"
	"onecloud/internal/pkg/storage"
	"onecloud/internal/repository"
	"onecloud/internal/svc"
	"onecloud/proto/.proto/service"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetPresignedURLLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetPresignedURLLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetPresignedURLLogic {
	return &GetPresignedURLLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// GetPresignedURL 获取预签名下载 URL
// 归属校验与下载接口一致（仅文件所有者）；本地驱动不支持时返回 40005，前端回落普通下载
func (l *GetPresignedURLLogic) GetPresignedURL(in *service.GetPresignedURLRequest) (*service.Response, error) {
	if err := in.Validate(); err != nil {
		return response.Err(errors.New("参数校验失败"))
	}

	userID, err := jwt.GetUserIdFromCtx(l.ctx)
	if err != nil {
		return response.Err(err)
	}

	fileRepo := repository.NewFileRepository(l.svcCtx.EntClient)
	f, err := fileRepo.GetInfo(l.ctx, in.GetFileId())
	if err != nil {
		if ent.IsNotFound(err) {
			return response.ErrWithCode(response.CodeFileNotFound, "文件不存在")
		}
		return response.ErrInternal(l.Logger, "查询文件失败", err)
	}
	if f.UserID != userID {
		return response.ErrWithCode(response.CodeFileNoPermission, "无权访问该文件")
	}
	// 回收站中的文件不可预签名
	if f.DeletedAt != nil {
		return response.ErrWithCode(response.CodeFileNotFound, "文件不存在")
	}

	// 有效期缺省 1 小时，钳制 [1min, 7d]：无上限时用户可请求十年有效的直链
	expire := int64(3600)
	if in.ExpireSeconds != nil && *in.ExpireSeconds > 0 {
		expire = *in.ExpireSeconds
	}
	const maxPresignExpire = int64(7 * 24 * 3600)
	if expire > maxPresignExpire {
		expire = maxPresignExpire
	}

	url, err := l.svcCtx.Storage.PresignURL(l.ctx, f.Path, expire, "download", f.Name)
	if err != nil {
		if errors.Is(err, storage.ErrNotImplemented) {
			return response.ErrWithCode(response.CodeStorageNotImpl, "当前存储驱动不支持预签名下载")
		}
		l.Errorf("生成预签名 URL 失败 file=%s: %v", f.ID, err)
		return response.ErrWithCode(response.CodeStorageRead, "预签名生成失败")
	}

	l.Infof("presign success user=%s file=%s expire=%ds", userID, f.ID, expire)
	return response.Success(&service.GetPresignedURLResponse{
		Url:      url,
		ExpireAt: time.Now().Add(time.Duration(expire) * time.Second).Unix(),
	})
}
