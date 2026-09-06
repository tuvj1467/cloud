package repository

import (
	"context"
	"fmt"

	"onecloud/ent"
	"onecloud/ent/filepart"
	"onecloud/internal/pkg/snowId"
	"onecloud/proto/.proto/service"
)

// FilePartRepository 分片上传数据访问层
type FilePartRepository struct {
	client *ent.Client
}

// NewFilePartRepository 创建分片仓库实例
func NewFilePartRepository(client *ent.Client) *FilePartRepository {
	return &FilePartRepository{client: client}
}

// CreateBatch 批量创建分片记录（初始化时预创建 total_parts 条 pending 记录）
func (r *FilePartRepository) CreateBatch(ctx context.Context, uploadID, userID, bucketID string, in *service.InitMultipartUploadRequest) error {
	bulk := make([]*ent.FilePartCreate, 0, int(in.GetTotalParts()))
	for i := 1; i <= int(in.GetTotalParts()); i++ {
		// file_parts.id 为无默认值的字符串主键，必须显式生成雪花 ID
		partID, err := snowId.Generate()
		if err != nil {
			return fmt.Errorf("生成分片记录 ID 失败: %w", err)
		}
		bulk = append(bulk, r.client.FilePart.Create().
			SetID(fmt.Sprintf("%d", partID)).
			SetUploadID(uploadID).
			SetUserID(userID).
			SetBucketID(bucketID).
			SetFileName(in.GetName()).
			SetFileSuffix(in.GetSuffix()).
			SetFileSize(in.GetSize()).
			SetTotalParts(int(in.GetTotalParts())).
			SetPartNumber(i).
			SetStatus(filepart.StatusPending))
	}
	_, err := r.client.FilePart.CreateBulk(bulk...).Save(ctx)
	if err != nil {
		return fmt.Errorf("批量创建分片记录失败: %w", err)
	}
	return nil
}

// GetByUploadAndNumber 根据 upload_id 与 part_number 查询分片
func (r *FilePartRepository) GetByUploadAndNumber(ctx context.Context, uploadID string, partNumber int) (*ent.FilePart, error) {
	return r.client.FilePart.Query().
		Where(
			filepart.UploadIDEQ(uploadID),
			filepart.PartNumberEQ(partNumber),
		).
		Only(ctx)
}

// ListByUploadID 查询某次上传的所有分片（按 part_number 升序）
func (r *FilePartRepository) ListByUploadID(ctx context.Context, uploadID string) ([]*ent.FilePart, error) {
	return r.client.FilePart.Query().
		Where(filepart.UploadIDEQ(uploadID)).
		Order(ent.Asc(filepart.FieldPartNumber)).
		All(ctx)
}

// MarkUploaded 标记分片为已上传，并记录大小、MD5、路径
func (r *FilePartRepository) MarkUploaded(ctx context.Context, id string, partSize int64, partMD5, partPath string) error {
	return r.client.FilePart.UpdateOneID(id).
		SetPartSize(partSize).
		SetPartMd5(partMD5).
		SetPartPath(partPath).
		SetStatus(filepart.StatusUploaded).
		Exec(ctx)
}

// MarkMerged 批量将分片状态更新为已合并
func (r *FilePartRepository) MarkMerged(ctx context.Context, uploadID string) error {
	_, err := r.client.FilePart.Update().
		Where(filepart.UploadIDEQ(uploadID)).
		SetStatus(filepart.StatusMerged).
		Save(ctx)
	return err
}

// CountUploaded 统计某次上传中已 uploaded 的分片数
func (r *FilePartRepository) CountUploaded(ctx context.Context, uploadID string) (int, error) {
	return r.client.FilePart.Query().
		Where(
			filepart.UploadIDEQ(uploadID),
			filepart.StatusEQ(filepart.StatusUploaded),
		).
		Count(ctx)
}

// DeleteByUploadID 按 upload_id 批量删除分片记录（取消上传时使用）
func (r *FilePartRepository) DeleteByUploadID(ctx context.Context, uploadID string) error {
	_, err := r.client.FilePart.Delete().
		Where(filepart.UploadIDEQ(uploadID)).
		Exec(ctx)
	return err
}

// GetMetaByUploadID 从分片表中获取上传元信息（任取一条记录即可）
// 用于 Redis 缓存未命中时兜底
func (r *FilePartRepository) GetMetaByUploadID(ctx context.Context, uploadID string) (*ent.FilePart, error) {
	return r.client.FilePart.Query().
		Where(filepart.UploadIDEQ(uploadID)).
		First(ctx)
}
