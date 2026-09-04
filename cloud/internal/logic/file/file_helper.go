package filelogic

import (
	"onecloud/ent"
	"onecloud/proto/.proto/service"
)

// toFileMeta 将 ent.File 转为 proto 的 FileMeta
func toFileMeta(f *ent.File) *service.FileMeta {
	return &service.FileMeta{
		FileId:      f.ID,
		Name:        f.Name,
		Path:        f.Path,
		Size:        f.Size,
		Suffix:      f.Ext,
		UserId:      f.UserID,
		BucketId:    f.BucketID,
		Md5:         f.Md5,
		ContentType: f.ContentType,
		CreatedAt:   f.CreatedAt.Format("2006-01-02 15:04:05"),
		UpdatedAt:   f.UpdatedAt.Format("2006-01-02 15:04:05"),
	}
}
