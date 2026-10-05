package filelogic

import (
	"context"
	"errors"
	"io"

	"onecloud/ent"
	"onecloud/internal/pkg/jwt"
	"onecloud/internal/pkg/response"
	"onecloud/internal/repository"
	"onecloud/internal/svc"
	"onecloud/proto/.proto/service"

	"github.com/zeromicro/go-zero/core/logx"
)

const downloadChunkSize = 64 * 1024 // 64KB 分块

type DownloadStreamLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewDownloadStreamLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DownloadStreamLogic {
	return &DownloadStreamLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// DownloadStream 流式下载：64KB 分块 server-streaming，避免整文件进内存
// 权限/归属校验与 DownloadFile 一致
func (l *DownloadStreamLogic) DownloadStream(in *service.DownloadFileRequest, stream service.File_DownloadStreamServer) error {
	if err := in.Validate(); err != nil {
		r, _ := response.Err(errors.New("参数校验失败"))
		return errors.New(r.Message)
	}

	userID, err := jwt.GetUserIdFromCtx(stream.Context())
	if err != nil {
		return err
	}

	fileRepo := repository.NewFileRepository(l.svcCtx.EntClient)
	f, err := fileRepo.GetInfo(l.ctx, in.GetFileId())
	if err != nil {
		if ent.IsNotFound(err) {
			r, _ := response.ErrWithCode(response.CodeFileNotFound, "文件不存在")
			return errors.New(r.Message)
		}
		return errors.New("查询文件失败: " + err.Error())
	}
	if f.UserID != userID {
		r, _ := response.ErrWithCode(response.CodeFileNoPermission, "无权下载该文件")
		return errors.New(r.Message)
	}
	// 回收站中的文件不可下载（还原后可恢复访问）
	if f.DeletedAt != nil {
		r, _ := response.ErrWithCode(response.CodeFileNotFound, "文件不存在")
		return errors.New(r.Message)
	}

	reader, err := l.svcCtx.Storage.Get(l.ctx, f.Path)
	if err != nil {
		l.Errorf("读取物理文件失败 path=%s: %v", f.Path, err)
		r, _ := response.ErrWithCode(response.CodeStorageRead, "文件读取失败")
		return errors.New(r.Message)
	}
	defer reader.Close()

	// 总大小优先取元数据，Stat 失败时退回 DB 记录值
	total := f.Size
	if info, err := l.svcCtx.Storage.Stat(l.ctx, f.Path); err == nil && info.Size > 0 {
		total = info.Size
	}

	buf := make([]byte, downloadChunkSize)
	var sent int64
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			sent += int64(n)
			if sendErr := stream.Send(&service.DownloadChunk{
				Data:  buf[:n],
				Sent:  sent,
				Total: total,
			}); sendErr != nil {
				return sendErr
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			l.Errorf("读取文件流失败 path=%s: %v", f.Path, err)
			r, _ := response.ErrWithCode(response.CodeStorageRead, "文件读取失败")
			return errors.New(r.Message)
		}
	}

	l.Infof("stream download success user=%s file=%s size=%d", userID, f.ID, sent)
	return nil
}
