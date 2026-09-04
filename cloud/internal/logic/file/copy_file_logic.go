package filelogic

import (
	"context"
	"errors"
	"fmt"
	"time"

	"onecloud/ent"
	"onecloud/internal/pkg/jwt"
	"onecloud/internal/pkg/response"
	"onecloud/internal/repository"
	"onecloud/internal/svc"
	"onecloud/proto/.proto/service"

	"github.com/zeromicro/go-zero/core/logx"
)

type CopyFileLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCopyFileLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CopyFileLogic {
	return &CopyFileLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// CopyFile 复制文件到目标桶（物理文件复制 + 新建元数据）
func (l *CopyFileLogic) CopyFile(in *service.CopyFileRequest) (*service.Response, error) {
	if err := in.Validate(); err != nil {
		return response.Err(errors.New("参数校验失败"))
	}

	userID, err := jwt.GetUserIdFromCtx(l.ctx)
	if err != nil {
		return response.Err(err)
	}

	bucketRepo := repository.NewBucketRepository(l.svcCtx.EntClient)
	fileRepo := repository.NewFileRepository(l.svcCtx.EntClient)

	// 1. 查询源文件并校验归属
	src, err := fileRepo.GetInfo(l.ctx, in.GetFileId())
	if err != nil {
		if ent.IsNotFound(err) {
			return response.Err(errors.New("源文件不存在"))
		}
		return response.Err(errors.New("查询源文件失败: " + err.Error()))
	}
	if src.UserID != userID {
		return response.Err(errors.New("无权复制该文件"))
	}

	// 2. 校验目标桶归属与容量
	targetBucket, err := bucketRepo.GetByID(l.ctx, in.GetTargetBucketId())
	if err != nil {
		if ent.IsNotFound(err) {
			return response.Err(errors.New("目标桶不存在"))
		}
		return response.Err(errors.New("查询目标桶失败: " + err.Error()))
	}
	if targetBucket.UserID != userID {
		return response.Err(errors.New("无权操作目标桶"))
	}
	if targetBucket.UsedSize+src.Size > targetBucket.SizeLimit {
		return response.Err(errors.New("目标桶容量不足"))
	}

	// 3. 生成新文件ID与存储路径
	newFileID := l.svcCtx.Snowflake.MustIdS()
	date := time.Now().Format("20060102")
	newPath := fmt.Sprintf("%s/%s/%s.%s", userID, date, newFileID, src.Ext)

	// 4. 复制物理文件（读取源文件流并写入新路径）
	reader, err := l.svcCtx.Storage.Get(l.ctx, src.Path)
	if err != nil {
		return response.Err(errors.New("读取源文件失败: " + err.Error()))
	}
	defer reader.Close()
	if err := l.svcCtx.Storage.Put(l.ctx, newPath, reader, src.Size); err != nil {
		return response.Err(errors.New("文件复制失败: " + err.Error()))
	}

	// 5. 创建新文件元数据
	newFile, err := fileRepo.CreateCopy(l.ctx, newFileID, userID, in.GetTargetBucketId(), in.GetNewName(), newPath, src)
	if err != nil {
		_ = l.svcCtx.Storage.Delete(l.ctx, newPath)
		return response.Err(errors.New("文件入库失败: " + err.Error()))
	}

	// 6. 扣减目标桶容量
	if err := bucketRepo.IncUsedSize(l.ctx, in.GetTargetBucketId(), src.Size); err != nil {
		l.Errorf("目标桶容量更新失败: %v", err)
	}

	return response.Success(&service.CopyFileResponse{
		File: toFileMeta(newFile),
	})
}
