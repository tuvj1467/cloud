package bucketlogic

import (
	"onecloud/ent"
	"onecloud/proto/.proto/service"
)

// toBucketMeta 将 ent.Bucket 转为 proto 的 BucketMeta
func toBucketMeta(b *ent.Bucket) *service.BucketMeta {
	return &service.BucketMeta{
		BucketId:    b.ID,
		Name:        b.Name,
		UserId:      b.UserID,
		Description: b.Description,
		SizeLimit:   b.SizeLimit,
		UsedSize:    b.UsedSize,
		Visibility:  string(b.Visibility),
		CreatedAt:   b.CreatedAt.Format("2006-01-02 15:04:05"),
		UpdatedAt:   b.UpdatedAt.Format("2006-01-02 15:04:05"),
	}
}
