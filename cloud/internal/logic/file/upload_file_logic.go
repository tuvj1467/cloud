package filelogic

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"onecloud/internal/pkg/jwt"
	"onecloud/internal/pkg/response"
	"onecloud/internal/pkg/util"
	"onecloud/internal/repository"
	"os"
	"path/filepath"
	"time"

	"onecloud/internal/svc"
	"onecloud/proto/.proto/service"

	"github.com/zeromicro/go-zero/core/logx"
)

type UploadFileLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewUploadFileLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UploadFileLogic {
	return &UploadFileLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *UploadFileLogic) validate(in *service.UploadFileRequest) error {
	if err := in.Validate(); err != nil {
		var vErr service.UploadFileRequestValidationError
		if errors.As(err, &vErr) {
			switch vErr.Field() {
			case "name":
				return errors.New("文件名为空或过长")
			case "suffix":
				return errors.New("缺少文件后缀")
			case "size":
				return errors.New("文件大小必须大于0")

			}
		}
		return errors.New("其他校验错误")
	}
	return nil
}

// 上传文件
func (l *UploadFileLogic) UploadFile(in *service.UploadFileRequest) (*service.Response, error) {
	//校验
	if err := l.validate(in); err != nil {
		return response.Err(err)
	}

	userID, err := jwt.GetUserIdFromCtx(l.ctx)
	if err != nil {
		return response.Err(err)
	}
	// 生成唯一文件ID
	var fileID string
	fileID = l.svcCtx.Snowflake.MustIdS()

	// 云存储路径：user_id/日期/file_id.suffix
	date := time.Now().Format("20060102")
	filePath := fmt.Sprintf("%s/%s/%s.%s", userID, date, fileID, in.Suffix)
	savePath := "./cloud_storage/" + filePath

	//转化为文件格式
	contentType := mime.TypeByExtension(in.GetSuffix())

	// 保存文件（本地模拟云存储）
	os.MkdirAll(filepath.Dir(savePath), 0755)
	os.WriteFile(savePath, in.Content, 0644)

	//计算md5
	md5 := util.CalculateFileMD5(in.GetContent())

	// 入库
	fileRepository := repository.NewFileRepository(l.svcCtx.EntClient)

	res, err := fileRepository.Creat(l.ctx, in, fileID, savePath, contentType, md5)
	if err != nil {
		return response.Err(err)
	}

	return response.Success(&service.UploadFileResponse{
		File: &service.FileMeta{
			FileId: res.ID, Name: res.Name, Path: res.Path, Suffix: res.Ext,
			CreatedAt: res.CreatedAt.String(), UpdatedAt: res.UpdatedAt.String(), Size: res.Size}})
}
