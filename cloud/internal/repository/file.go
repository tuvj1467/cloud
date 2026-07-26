package repository

import (
	"context"
	"database/sql"
	"onecloud/ent/ent"
	"onecloud/ent/ent/file"
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

// 获取 userId 下所有文件元信息
func (l *FileRepository) GetAllFileList(ctx context.Context, userId string) ([]*ent.File, error) {
	res, err := l.client.File.Query().Where(file.UserIDEQ(userId)).All(ctx)
	if err != nil {
		return nil, err
	}

	return res, nil

}

// 分页获取 userId 下文件元信息
func (l *FileRepository) GetPageFileList(ctx context.Context, userId string, in *service.FileListRequest) ([]*ent.File, error) {
	page := in.Page
	if page <= 0 {
		page = 1
	}
	pageSize := in.PageSize
	if pageSize <= 0 {
		pageSize = 10
	}
	offset := (page - 1) * pageSize
	query := l.client.File.Query().Where(file.UserIDEQ(userId))

	sortBy := strings.ToLower(in.SortBy)
	sortOrder := strings.ToLower(in.SortOrder)
	isAsc := sortOrder == "asc"

	switch sortBy {
	case "name":
		if isAsc {
			query = query.Order(file.ByName(entsql.OrderAsc()))
		} else {
			query = query.Order(file.ByName(entsql.OrderDesc()))
		}
	case "size":
		if isAsc {
			query = query.Order(file.BySize(entsql.OrderAsc()))
		} else {
			query = query.Order(file.BySize(entsql.OrderDesc()))
		}
	case "create_time":
		if isAsc {
			query = query.Order(file.ByCreatedAt(entsql.OrderAsc()))
		} else {
			query = query.Order(file.ByCreatedAt(entsql.OrderDesc()))
		}
	default:
		// 默认按更新时间倒序，保证列表稳定
		query = query.Order(file.ByUpdatedAt(entsql.OrderDesc()))
	}
	res, err := query.Offset(int(offset)).Limit(int(pageSize)).All(ctx)
	if err != nil {
		return nil, err
	}
	return res, nil

}

// 获取 bucket 文件元信息
func (l *FileRepository) GetBucketFileList(ctx context.Context, userId, bucketId string) ([]*ent.File, error) {
	res, err := l.client.File.Query().Where(file.UserIDEQ(userId), file.BucketIDEQ(bucketId)).All(ctx)
	if err != nil {
		return nil, err
	}

	return res, nil

}
