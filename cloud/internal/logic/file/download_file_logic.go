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

type DownloadFileLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewDownloadFileLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DownloadFileLogic {
	return &DownloadFileLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// unaryDownloadMaxBytes 一元下载接口的大小上限：超过此大小的文件整文件读入内存
// 再经 protobuf/JSON 序列化会产生数倍内存放大，应改走流式端点 DownloadStream
const unaryDownloadMaxBytes = 5 * 1024 * 1024

// DownloadFile 下载文件（一元接口，整文件返回；大文件请走 DownloadStream 流式端点）
func (l *DownloadFileLogic) DownloadFile(in *service.DownloadFileRequest) (*service.Response, error) {
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
		return response.Err(errors.New("查询文件失败: " + err.Error()))
	}

	// 校验文件归属
	if f.UserID != userID {
		return response.ErrWithCode(response.CodeFileNoPermission, "无权下载该文件")
	}

	// 大小治理：超上限走流式端点，避免大文件整文件驻内存
	if f.Size > unaryDownloadMaxBytes {
		return response.ErrWithCode(response.CodeFileTooLarge,
			"文件过大，请使用流式下载接口 download_stream")
	}

	// 通过存储驱动读取物理文件
	reader, err := l.svcCtx.Storage.Get(l.ctx, f.Path)
	if err != nil {
		l.Errorf("读取物理文件失败 path=%s: %v", f.Path, err)
		return response.ErrWithCode(response.CodeStorageRead, "文件读取失败: "+err.Error())
	}
	defer reader.Close()

	content, err := io.ReadAll(io.LimitReader(reader, unaryDownloadMaxBytes+1))
	if err != nil {
		l.Errorf("读取文件流失败 path=%s: %v", f.Path, err)
		return response.ErrWithCode(response.CodeStorageRead, "文件读取失败: "+err.Error())
	}

	l.Infof("download success user=%s file=%s size=%d", userID, f.ID, f.Size)

	return response.Success(&service.DownloadFileResponse{
		Content: content,
		File:    toFileMeta(f),
	})
}
