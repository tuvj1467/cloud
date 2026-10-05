package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"onecloud/ent"
	"onecloud/ent/bucket"
	"onecloud/ent/file"
	"onecloud/ent/fileshare"
	"onecloud/ent/predicate"
	"onecloud/internal/pkg/storage"
	"onecloud/proto/.proto/service"

	entsql "entgo.io/ent/dialect/sql"

	"github.com/zeromicro/go-zero/core/logx"
)

// FileRepository 文件数据访问层
type FileRepository struct {
	client *ent.Client
}

// NewFileRepository 创建文件仓库实例
func NewFileRepository(client *ent.Client) *FileRepository {
	return &FileRepository{client: client}
}

// Create 创建文件记录（写入 user_id 与 bucket_id，保证桶归属）
func (l *FileRepository) Create(ctx context.Context, in *service.UploadFileRequest, id, userID, bucketID, path, contentType, md5 string) (*ent.File, error) {
	return l.client.File.Create().
		SetID(id).
		SetName(in.GetName()).
		SetPath(path).
		SetMd5(md5).
		SetExt(in.GetSuffix()).
		SetContentType(contentType).
		SetSize(in.GetSize()).
		SetUserID(userID).
		SetBucketID(bucketID).
		Save(ctx)
}

// GetInfo 根据 id 获取文件元信息
func (l *FileRepository) GetInfo(ctx context.Context, id string) (*ent.File, error) {
	return l.client.File.Get(ctx, id)
}

// GetByMD5 根据 user_id 与 md5 查询文件（秒传检查）
func (l *FileRepository) GetByMD5(ctx context.Context, userID, md5 string) (*ent.File, error) {
	// 秒传兜底查询不得命中回收站中的文件
	return l.client.File.Query().
		Where(file.UserIDEQ(userID), file.Md5EQ(md5), file.DeletedAtIsNil()).
		First(ctx)
}

// GetAllFileList 获取 userId 下所有文件元信息
func (l *FileRepository) GetAllFileList(ctx context.Context, userId string) ([]*ent.File, error) {
	res, err := l.client.File.Query().Where(file.UserIDEQ(userId), file.DeletedAtIsNil()).All(ctx)
	if err != nil {
		return nil, err
	}

	return res, nil
}

// GetPageFileList 分页获取 userId 下文件元信息，返回列表与总数
func (l *FileRepository) GetPageFileList(ctx context.Context, userId string, in *service.FileListRequest) ([]*ent.File, int, error) {
	page := in.Page
	if page <= 0 {
		page = 1
	}
	pageSize := in.PageSize
	if pageSize <= 0 {
		pageSize = 10
	}
	offset := (page - 1) * pageSize

	// 基础查询条件：按用户隔离 + 排除回收站 + 可选文件名模糊过滤
	basePredicates := []predicate.File{file.UserIDEQ(userId), file.DeletedAtIsNil()}
	if in.NameKeyword != nil && *in.NameKeyword != "" {
		basePredicates = append(basePredicates, file.NameContains(*in.NameKeyword))
	}

	// 计算总数
	count, err := l.client.File.Query().Where(basePredicates...).Count(ctx)
	if err != nil {
		return nil, 0, err
	}

	// 构造分页查询
	query := l.client.File.Query().Where(basePredicates...)

	query = applyFileSort(query, in.SortBy, in.SortOrder)

	res, err := query.Offset(int(offset)).Limit(int(pageSize)).All(ctx)
	if err != nil {
		return nil, 0, err
	}
	return res, count, nil
}

// GetBucketFileList 分页获取指定桶下文件元信息，返回列表与总数
func (l *FileRepository) GetBucketFileList(ctx context.Context, userId, bucketId string, in *service.BucketFileListRequest) ([]*ent.File, int, error) {
	page := in.Page
	if page <= 0 {
		page = 1
	}
	pageSize := in.PageSize
	if pageSize <= 0 {
		pageSize = 10
	}
	offset := (page - 1) * pageSize

	basePredicates := []predicate.File{
		file.UserIDEQ(userId),
		file.BucketIDEQ(bucketId),
		file.DeletedAtIsNil(),
	}
	if in.NameKeyword != nil && *in.NameKeyword != "" {
		basePredicates = append(basePredicates, file.NameContains(*in.NameKeyword))
	}

	count, err := l.client.File.Query().Where(basePredicates...).Count(ctx)
	if err != nil {
		return nil, 0, err
	}

	query := l.client.File.Query().Where(basePredicates...)

	query = applyFileSort(query, in.SortBy, in.SortOrder)

	res, err := query.Offset(int(offset)).Limit(int(pageSize)).All(ctx)
	if err != nil {
		return nil, 0, err
	}
	return res, count, nil
}

// Rename 重命名文件（仅修改 name 字段）
func (l *FileRepository) Rename(ctx context.Context, id, newName string) (*ent.File, error) {
	return l.client.File.UpdateOneID(id).
		SetName(newName).
		Save(ctx)
}

// Delete 删除文件记录
func (l *FileRepository) Delete(ctx context.Context, id string) error {
	return l.client.File.DeleteOneID(id).Exec(ctx)
}

// ListDeleted 分页查询用户回收站（按删除时间倒序），返回列表与总数
func (l *FileRepository) ListDeleted(ctx context.Context, userID string, page, pageSize int) ([]*ent.File, int, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 10
	}
	base := l.client.File.Query().
		Where(file.UserIDEQ(userID), file.DeletedAtNotNil())
	total, err := base.Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	list, err := base.Order(file.ByDeletedAt(entsql.OrderDesc())).
		Offset((page - 1) * pageSize).Limit(pageSize).All(ctx)
	if err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

// RestoreByIDs 恢复本人的一组回收站文件（置空 deleted_at）
// 返回实际恢复数；不属于本人或不在回收站的 ID 自动跳过
func (l *FileRepository) RestoreByIDs(ctx context.Context, userID string, ids []string) (int, error) {
	return l.client.File.Update().
		Where(
			file.IDIn(ids...),
			file.UserIDEQ(userID),
			file.DeletedAtNotNil(),
		).
		ClearDeletedAt().
		Save(ctx)
}

// ListDeletedByIDs 加载本人回收站中的指定文件（Purge 前置校验与事后清理用）
func (l *FileRepository) ListDeletedByIDs(ctx context.Context, userID string, ids []string) ([]*ent.File, error) {
	return l.client.File.Query().
		Where(
			file.IDIn(ids...),
			file.UserIDEQ(userID),
			file.DeletedAtNotNil(),
		).
		All(ctx)
}

// ListStaleDeleted 加载软删时间早于 before 的文件（回收站 30 天自动清理用），限 limit 条
func (l *FileRepository) ListStaleDeleted(ctx context.Context, before time.Time, limit int) ([]*ent.File, error) {
	return l.client.File.Query().
		Where(
			file.DeletedAtNotNil(),
			file.DeletedAtLT(before),
		).
		Order(file.ByDeletedAt(entsql.OrderAsc())).
		Limit(limit).
		All(ctx)
}

// SoftDelete 软删除：标记 deleted_at（回收站语义）
// 配额不归还、物理文件不删除——恢复即还原，彻底删除（Purge）时才执行归还与物理清理
func (l *FileRepository) SoftDelete(ctx context.Context, id string) error {
	now := time.Now()
	return l.client.File.UpdateOneID(id).
		SetDeletedAt(now).
		SetUpdatedAt(now).
		Exec(ctx)
}

// UpdateSize 更新文件大小（分片合并后使用）
func (l *FileRepository) UpdateSize(ctx context.Context, id string, size int64) error {
	return l.client.File.UpdateOneID(id).
		SetSize(size).
		Exec(ctx)
}

// CreateCopy 复制文件元数据（用于文件复制操作，复用源文件的 md5/content_type/ext）
func (l *FileRepository) CreateCopy(ctx context.Context, id, userID, bucketID, name, path string, src *ent.File) (*ent.File, error) {
	return l.client.File.Create().
		SetID(id).
		SetName(name).
		SetPath(path).
		SetMd5(src.Md5).
		SetExt(src.Ext).
		SetContentType(src.ContentType).
		SetSize(src.Size).
		SetUserID(userID).
		SetBucketID(bucketID).
		Save(ctx)
}

// CreateMultipart 创建分片上传合并后的文件元数据
// 与 Create 的区别：入参是分片合并后的零散字段，而非 UploadFileRequest
func (l *FileRepository) CreateMultipart(
	ctx context.Context,
	id, userID, bucketID, name, path, suffix, contentType, md5 string,
	size int64,
) (*ent.File, error) {
	return l.client.File.Create().
		SetID(id).
		SetName(name).
		SetPath(path).
		SetMd5(md5).
		SetExt(suffix).
		SetContentType(contentType).
		SetSize(size).
		SetUserID(userID).
		SetBucketID(bucketID).
		Save(ctx)
}

// DeleteFull 事务删除文件：文件记录 + 关联分享 + 桶容量原子归还
// 事务提交后由调用方清理 Redis 索引与物理文件（物理孤儿可容忍，DB 必须一致）
func (l *FileRepository) DeleteFull(ctx context.Context, f *ent.File) error {
	tx, err := l.client.Tx(ctx)
	if err != nil {
		return err
	}
	// 1. 级联删除关联分享记录
	if _, err := tx.FileShare.Delete().Where(fileshare.FileIDEQ(f.ID)).Exec(ctx); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("删除关联分享失败: %w", err)
	}
	// 2. 删除文件记录
	if err := tx.File.DeleteOneID(f.ID).Exec(ctx); err != nil {
		_ = tx.Rollback()
		return err
	}
	// 3. 事务内原子归还桶容量（保证 used_size 不为负）
	if f.BucketID != "" && f.Size > 0 {
		affected, err := tx.Bucket.Update().
			Where(
				bucket.IDEQ(f.BucketID),
				func(s *entsql.Selector) {
					s.Where(entsql.ExprP("used_size >= ?", f.Size))
				},
			).
			AddUsedSize(-f.Size).
			AddVersion(1).
			Save(ctx)
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("归还桶容量失败: %w", err)
		}
		if affected == 0 {
			// 账目漂移（used_size < f.Size）时归还 0 行：不阻塞删除，留痕供对账
			logx.Errorf("归还桶容量 0 行（账目漂移?）bucket=%s file=%s size=%d", f.BucketID, f.ID, f.Size)
		}
	}
	return tx.Commit()
}

// DeletePhysicalIfUnshared 安全物理删除：秒传允许跨桶/跨记录共享物理路径，
// 仍有其他记录引用该路径时跳过删除（调用方须在删除 DB 记录之后调用，
// 否则计数含自身恒 >0）。计数失败时保守保留物理文件——宁可留孤儿待 GC，不误删共享文件
func (r *FileRepository) DeletePhysicalIfUnshared(ctx context.Context, driver storage.Driver, path string) error {
	n, err := r.client.File.Query().Where(file.PathEQ(path)).Count(ctx)
	if err != nil {
		return fmt.Errorf("查询共享引用失败（保守保留物理文件）: %w", err)
	}
	if n > 0 {
		return nil
	}
	return driver.Delete(ctx, path)
}

// applyFileSort 列表排序的统一实现（用户级/桶级共用）：sortBy ∈ {name,size,create_time}，
// 其余（含空）按更新时间倒序保证列表稳定
func applyFileSort(query *ent.FileQuery, sortBy, sortOrder string) *ent.FileQuery {
	isAsc := strings.ToLower(sortOrder) == "asc"
	switch strings.ToLower(sortBy) {
	case "name":
		if isAsc {
			return query.Order(file.ByName(entsql.OrderAsc()))
		}
		return query.Order(file.ByName(entsql.OrderDesc()))
	case "size":
		if isAsc {
			return query.Order(file.BySize(entsql.OrderAsc()))
		}
		return query.Order(file.BySize(entsql.OrderDesc()))
	case "create_time":
		if isAsc {
			return query.Order(file.ByCreatedAt(entsql.OrderAsc()))
		}
		return query.Order(file.ByCreatedAt(entsql.OrderDesc()))
	default:
		return query.Order(file.ByUpdatedAt(entsql.OrderDesc()))
	}
}

// GetPageFileList
