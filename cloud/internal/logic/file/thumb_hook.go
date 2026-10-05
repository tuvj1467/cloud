package filelogic

import (
	"context"
	"os"
	"path/filepath"

	"onecloud/internal/pkg/storage"
	"onecloud/internal/pkg/thumb"
	"onecloud/internal/svc"
)

// ThumbMinSize 低于该阈值的文件不生成缩略图（小文件直接用原图，避免无谓的解码开销）
const ThumbMinSize = 50 * 1024

// BuildThumb 为图片文件生成缩略图（仅本地驱动；MinIO 驱动 TODO: 生成后上传缩略图对象）。
// 缩略图统一存为 {RootDir}/.thumbs/{fileID}.jpg（最长边 320、质量 80）。
// 任何失败只记日志返回 false——缩略图属增强能力，绝不影响上传主流程。
func BuildThumb(ctx context.Context, svcCtx *svc.ServiceContext, fileID, srcPath, ext string) bool {
	ld, ok := svcCtx.Storage.(*storage.LocalDriver)
	if !ok {
		return false // MinIO/S3 驱动暂不生成缩略图
	}
	if !storage.ValidSuffix(ext) || !thumb.IsSupported(ext) {
		return false
	}
	thumbPath := filepath.Join(ld.RootDir(), ".thumbs", fileID+".jpg")
	src := filepath.Join(ld.RootDir(), filepath.FromSlash(srcPath))
	return thumb.GenerateJPEG(src, thumbPath, 320, 80) == nil
}

// thumbPath 缩略图物理路径；不存在时返回空串
func thumbPath(svcCtx *svc.ServiceContext, fileID string) string {
	ld, ok := svcCtx.Storage.(*storage.LocalDriver)
	if !ok {
		return ""
	}
	p := filepath.Join(ld.RootDir(), ".thumbs", fileID+".jpg")
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}
