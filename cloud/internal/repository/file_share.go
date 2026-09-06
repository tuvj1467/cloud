package repository

import (
	"context"
	"fmt"

	"onecloud/ent"
	"onecloud/ent/fileshare"
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

// IncUsedCount 递增已访问次数（乐观锁）
// 返回递增后的新值
func (r *FileShareRepository) IncUsedCount(ctx context.Context, id string, version int) (int, error) {
	// 先读取当前 used_count
	share, err := r.client.FileShare.Get(ctx, id)
	if err != nil {
		return 0, err
	}
	newCount := share.UsedCount + 1

	affected, err := r.client.FileShare.Update().
		Where(
			fileshare.IDEQ(id),
			fileshare.VersionEQ(version),
		).
		SetUsedCount(newCount).
		SetVersion(version + 1).
		Save(ctx)
	if err != nil {
		return 0, fmt.Errorf("更新访问次数失败: %w", err)
	}
	if affected == 0 {
		return 0, fmt.Errorf("分享记录版本冲突，请重试")
	}
	return newCount, nil
}

// DeleteByID 删除分享记录
func (r *FileShareRepository) DeleteByID(ctx context.Context, id string) error {
	return r.client.FileShare.DeleteOneID(id).Exec(ctx)
}

// ListByFileIDs 查询一组文件的分享记录（事务删除前查出，供事务后清理 Redis）
func (r *FileShareRepository) ListByFileIDs(ctx context.Context, fileIDs []string) ([]*ent.FileShare, error) {
	return r.client.FileShare.Query().Where(fileshare.FileIDIn(fileIDs...)).All(ctx)
}
