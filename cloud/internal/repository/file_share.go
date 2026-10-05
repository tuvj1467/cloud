package repository

import (
	"context"
	"fmt"

	"onecloud/ent"
	"onecloud/ent/fileshare"

	entsql "entgo.io/ent/dialect/sql"
)

// FileShareRepository 文件分享数据访问层
type FileShareRepository struct {
	client *ent.Client
}

// NewFileShareRepository 创建分享仓库实例
func NewFileShareRepository(client *ent.Client) *FileShareRepository {
	return &FileShareRepository{client: client}
}

// CreateWithParams 通过参数创建分享记录
func (r *FileShareRepository) CreateWithParams(
	ctx context.Context,
	id, fileID, userID, shareCode, permission string,
	expireAt *int64, // unix 秒；nil 表示永久
	maxCount *int,
) (*ent.FileShare, error) {
	builder := r.client.FileShare.Create().
		SetID(id).
		SetFileID(fileID).
		SetUserID(userID).
		SetShareCode(shareCode).
		SetPermission(fileshare.Permission(permission))

	if expireAt != nil {
		builder = builder.SetExpireAt(timeFromUnix(*expireAt))
	}
	if maxCount != nil {
		builder = builder.SetMaxCount(*maxCount)
	}

	return builder.Save(ctx)
}

// GetByCode 根据 share_code 查询分享记录
func (r *FileShareRepository) GetByCode(ctx context.Context, code string) (*ent.FileShare, error) {
	return r.client.FileShare.Query().
		Where(fileshare.ShareCodeEQ(code)).
		Only(ctx)
}

// IncUsedCount 原子递增已访问次数
// 单条 UPDATE 自增（used_count = used_count + 1），无需先读后写：
// 并发下载不会因版本冲突丢失递增（原乐观锁实现在冲突时放弃递增，
// Redis 过期后以偏小的 DB 值重建计数，max_count 上限可被突破）
// 返回递增后的新值
func (r *FileShareRepository) IncUsedCount(ctx context.Context, id string) (int, error) {
	share, err := r.client.FileShare.UpdateOneID(id).
		AddUsedCount(1).
		Save(ctx)
	if err != nil {
		return 0, fmt.Errorf("更新访问次数失败: %w", err)
	}
	return share.UsedCount, nil
}

// DeleteByID 删除分享记录
func (r *FileShareRepository) DeleteByID(ctx context.Context, id string) error {
	return r.client.FileShare.DeleteOneID(id).Exec(ctx)
}

// ListByUser 分页查询用户的分享记录（创建时间倒序），返回列表与总数
func (r *FileShareRepository) ListByUser(ctx context.Context, userID string, page, pageSize int) ([]*ent.FileShare, int, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 10
	}
	base := r.client.FileShare.Query().Where(fileshare.UserIDEQ(userID))
	total, err := base.Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	list, err := base.Order(fileshare.ByCreatedAt(entsql.OrderDesc())).Offset((page - 1) * pageSize).Limit(pageSize).All(ctx)
	if err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

// ListByFileIDs 查询一组文件的分享记录（事务删除前查出，供事务后清理 Redis）
func (r *FileShareRepository) ListByFileIDs(ctx context.Context, fileIDs []string) ([]*ent.FileShare, error) {
	return r.client.FileShare.Query().Where(fileshare.FileIDIn(fileIDs...)).All(ctx)
}
