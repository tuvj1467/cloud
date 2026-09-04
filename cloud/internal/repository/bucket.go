package repository

import (
	"context"
	"database/sql"
	"fmt"

	"onecloud/ent"
	"onecloud/ent/bucket"
	"onecloud/ent/file"
	"onecloud/proto/.proto/service"

	entsql "entgo.io/ent/dialect/sql"
)

// BucketRepository 桶数据访问层
type BucketRepository struct {
	client *ent.Client
	db     *sql.DB
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

// IncUsedSize 增减桶已用容量（乐观锁：版本号不匹配则失败）
// delta 为正表示增加，为负表示减少
func (r *BucketRepository) IncUsedSize(ctx context.Context, bucketID string, delta int64) error {
	// 先查询当前版本
	b, err := r.client.Bucket.Get(ctx, bucketID)
	if err != nil {
		return fmt.Errorf("查询桶失败: %w", err)
	}
	newUsed := b.UsedSize + delta
	if newUsed < 0 {
		newUsed = 0
	}
	// 带版本号条件更新（乐观锁）
	affected, err := r.client.Bucket.Update().
		Where(
			bucket.IDEQ(bucketID),
			bucket.VersionEQ(b.Version),
		).
		SetUsedSize(newUsed).
		SetVersion(b.Version + 1).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("更新桶容量失败: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("桶容量更新冲突（版本号不匹配），请重试")
	}
	return nil
}
