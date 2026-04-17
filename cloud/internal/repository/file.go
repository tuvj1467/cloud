package repository

import (
	"context"
	"database/sql"
	"onecloud/ent/ent"
	"onecloud/proto/.proto/service"
)

type FileRepository struct {
	client *ent.Client
	db     *sql.DB
}

func NewFileRepository(client *ent.Client) *FileRepository {
	return &FileRepository{client: client}
}

func (l *FileRepository) Creat(ctx context.Context, in *service.UploadFileRequest, id, path, fileType, md5 string) (*ent.File, error) {
	return l.client.File.Create().
		SetID(id).
		SetName(in.GetName()).
		SetPath(path).
		SetMd5(md5).
		SetExt(in.GetSuffix()).
		SetContentType(fileType).
		SetSize(in.GetSize()).
		Save(ctx)
}

func (l *FileRepository) GetInfo(ctx context.Context, id string) (*ent.File, error) {
	return l.client.File.Get(ctx, id)
}

func (l *FileRepository) GetFileList(ctx context.Context, userId string) (*ent.File, error) {
	return l.client.File.Query().Where(File.use)
}
