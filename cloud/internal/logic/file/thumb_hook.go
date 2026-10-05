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

// thumbSem 限制并发缩略图生成数（解码 + CatmullRom 缩放为 CPU/内存密集操作，
// 批量上传场景下防止 goroutine 与内存尖峰）；满载时放弃异步生成，
// 由 GetFileThumb 的按需自愈路径兜底
var thumbSem = make(chan struct{}, 4)

// BuildThumbAsync 异步生成缩略图：主请求立即返回，不阻塞上传/合并响应
// （同步生成时大图解码会把上传响应拖慢数百毫秒到秒级）
func BuildThumbAsync(ctx context.Context, svcCtx *svc.ServiceContext, fileID, srcPath, ext string) {
	go func() {
		select {
		case thumbSem <- struct{}{}:
			defer func() { <-thumbSem }()
			// 脱离请求生命周期（响应返回后请求 ctx 会被取消）
			BuildThumb(context.WithoutCancel(ctx), svcCtx, fileID, srcPath, ext)
		default:
		}
	}()
}

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
