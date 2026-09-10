package repository

import (
	"context"
	"fmt"

	"onecloud/ent"
	"onecloud/ent/bucket"
	"onecloud/ent/file"
	"onecloud/ent/fileshare"
	"onecloud/proto/.proto/service"

	entsql "entgo.io/ent/dialect/sql"
)

// BucketRepository 桶数据访问层
type BucketRepository struct {
	client *ent.Client
}

// NewBucketRepository 创建桶仓库实例
func NewBucketRepository(client *ent.Client) *BucketRepository {
	return &BucketRepository{client: client}
}

// parseVisibility 将字符串转为 ent 的 Visibility 枚举
func parseVisibility(v string) bucket.Visibility {
	switch v {
	case "internal":
		return bucket.VisibilityInternal
	case "public":
		return bucket.VisibilityPublic
	default:
		return bucket.VisibilityPrivate
	}
}

// Create 创建桶
func (r *BucketRepository) Create(ctx context.Context, id, userID string, in *service.CreateBucketRequest) (*ent.Bucket, error) {
	sizeLimit := in.GetSizeLimit()
	if sizeLimit <= 0 {
		sizeLimit = 1024 * 1024 * 1024 // 默认 1GB
	}
	builder := r.client.Bucket.Create().
		SetID(id).
		SetName(in.GetName()).
		SetUserID(userID).
		SetSizeLimit(sizeLimit).
		SetUsedSize(0).
		SetVisibility(parseVisibility(in.GetVisibility()))

	// description 为 optional，仅在实际赋值时写入
	if in.Description != nil {
		builder.SetDescription(in.GetDescription())
	}
	return builder.Save(ctx)
}

// GetByID 根据 id 获取桶
func (r *BucketRepository) GetByID(ctx context.Context, id string) (*ent.Bucket, error) {
	return r.client.Bucket.Get(ctx, id)
}

// GetByUserAndName 根据用户ID与桶名查询（唯一性检查）
func (r *BucketRepository) GetByUserAndName(ctx context.Context, userID, name string) (*ent.Bucket, error) {
	return r.client.Bucket.Query().
		Where(bucket.UserIDEQ(userID), bucket.NameEQ(name)).
		Only(ctx)
}

// GetDefaultBucket 获取用户默认桶，不存在返回 nil
func (r *BucketRepository) GetDefaultBucket(ctx context.Context, userID string) (*ent.Bucket, error) {
	return r.client.Bucket.Query().
		Where(bucket.UserIDEQ(userID), bucket.NameEQ("default")).
		Only(ctx)
}

// ListByUser 分页获取用户桶列表，返回列表与总数
func (r *BucketRepository) ListByUser(ctx context.Context, userID string, page, pageSize int32) ([]*ent.Bucket, int, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 10
	}
	offset := (page - 1) * pageSize

	count, err := r.client.Bucket.Query().Where(bucket.UserIDEQ(userID)).Count(ctx)
	if err != nil {
		return nil, 0, err
	}

	res, err := r.client.Bucket.Query().
		Where(bucket.UserIDEQ(userID)).
		Order(bucket.ByUpdatedAt(entsql.OrderDesc())).
		Offset(int(offset)).Limit(int(pageSize)).
		All(ctx)
	if err != nil {
		return nil, 0, err
	}
	return res, count, nil
}

// Update 更新桶（描述、容量、权限）
func (r *BucketRepository) Update(ctx context.Context, id string, in *service.UpdateBucketRequest) (*ent.Bucket, error) {
	builder := r.client.Bucket.UpdateOneID(id)
	// optional 字段通过指针判断是否赋值
	if in.Description != nil {
		builder.SetNillableDescription(in.Description)
	}
	if in.SizeLimit != nil {
		builder.SetNillableSizeLimit(in.SizeLimit)
	}
	if in.Visibility != nil {
		vis := parseVisibility(in.GetVisibility())
		builder.SetNillableVisibility(&vis)
	}
	return builder.Save(ctx)
}

// Delete 删除桶
func (r *BucketRepository) Delete(ctx context.Context, id string) error {
	return r.client.Bucket.DeleteOneID(id).Exec(ctx)
}

// CountFiles 统计桶内文件数量
func (r *BucketRepository) CountFiles(ctx context.Context, bucketID string) (int, error) {
	return r.client.File.Query().Where(file.BucketIDEQ(bucketID)).Count(ctx)
}

// CountFilesGroupByBucket 单条 GROUP BY 聚合用户各桶的文件数（bucket_id → count）
func (r *BucketRepository) CountFilesGroupByBucket(ctx context.Context, userID string) (map[string]int64, error) {
	var rows []struct {
		BucketID string `json:"bucket_id"`
		Count    int64  `json:"count"`
	}
	err := r.client.File.Query().
		Where(file.UserIDEQ(userID)).
		GroupBy(file.FieldBucketID).
		Aggregate(ent.Count()).
		Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}
	res := make(map[string]int64, len(rows))
	for _, row := range rows {
		res[row.BucketID] = row.Count
	}
	return res, nil
}

// TryIncUsedSize 原子增加桶已用容量：单条 UPDATE 内联配额条件，杜绝并发超卖
// delta 必须为正；返回 false 表示配额不足或桶不存在
func (r *BucketRepository) TryIncUsedSize(ctx context.Context, bucketID string, delta int64) (bool, error) {
	if delta <= 0 {
		return false, fmt.Errorf("配额增量必须为正: %d", delta)
	}
	affected, err := r.client.Bucket.Update().
		Where(
			bucket.IDEQ(bucketID),
			func(s *entsql.Selector) {
				s.Where(entsql.ExprP("size_limit - used_size >= ?", delta))
			},
		).
		AddUsedSize(delta).
		AddVersion(1).
		Save(ctx)
	if err != nil {
		return false, fmt.Errorf("更新桶容量失败: %w", err)
	}
	return affected > 0, nil
}

// TryDecUsedSize 原子归还桶容量：保证 used_size 永不为负
// delta 必须为正（方法内部取 -delta 扣减）；返回 false 表示桶不存在或当前用量小于 delta（账目漂移时不强行扣减）
// 当前删除路径在事务内联归还（DeleteFull/DeleteWithFiles），本方法保留为事务外归还配额的公共 API。
func (r *BucketRepository) TryDecUsedSize(ctx context.Context, bucketID string, delta int64) (bool, error) {
	if delta <= 0 {
		return false, fmt.Errorf("配额增量必须为正: %d", delta)
	}
	affected, err := r.client.Bucket.Update().
		Where(
			bucket.IDEQ(bucketID),
			func(s *entsql.Selector) {
				s.Where(entsql.ExprP("used_size >= ?", delta))
			},
		).
		AddUsedSize(-delta).
		AddVersion(1).
		Save(ctx)
	if err != nil {
		return false, fmt.Errorf("归还桶容量失败: %w", err)
	}
	return affected > 0, nil
}

// DeleteIfEmpty 事务内校验桶为空后删除（收缩 count 与 delete 的竞态窗口）
// 返回 false 表示桶非空
func (r *BucketRepository) DeleteIfEmpty(ctx context.Context, bucketID string) (bool, error) {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return false, err
	}
	count, err := tx.File.Query().Where(file.BucketIDEQ(bucketID)).Count(ctx)
	if err != nil {
		_ = tx.Rollback()
		return false, err
	}
	if count > 0 {
		_ = tx.Rollback()
		return false, nil
	}
	if err := tx.Bucket.DeleteOneID(bucketID).Exec(ctx); err != nil {
		_ = tx.Rollback()
		return false, err
	}
	return true, tx.Commit()
}

// DeleteWithFiles 事务删除桶 + 桶内全部文件 + 关联分享，返回文件列表供事务后清理
func (r *BucketRepository) DeleteWithFiles(ctx context.Context, bucketID string) ([]*ent.File, error) {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	// 先查出文件（事务提交后逐个清理物理文件与 Redis）
	files, err := tx.File.Query().Where(file.BucketIDEQ(bucketID)).All(ctx)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if len(files) > 0 {
		fileIDs := make([]string, 0, len(files))
		for _, f := range files {
			fileIDs = append(fileIDs, f.ID)
		}
		if _, err := tx.FileShare.Delete().Where(fileshare.FileIDIn(fileIDs...)).Exec(ctx); err != nil {
			_ = tx.Rollback()
			return nil, fmt.Errorf("删除关联分享失败: %w", err)
		}
		if _, err := tx.File.Delete().Where(file.BucketIDEQ(bucketID)).Exec(ctx); err != nil {
			_ = tx.Rollback()
			return nil, err
		}
	}
	if err := tx.Bucket.DeleteOneID(bucketID).Exec(ctx); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	return files, tx.Commit()
}
