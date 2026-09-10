// Package thumb 提供图片缩略图生成（仅本地驱动场景，MinIO 驱动跳过）
package thumb

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
	_ "image/gif"
)

// ErrUnsupportedImage 非可解码图片
var ErrUnsupportedImage = errors.New("thumb: 无法解码的图片格式")

// GenerateJPEG 解码 src 图片，将最长边缩放至 ≤maxEdge（不放大），以 JPEG quality 质量写入 dst
// 支持格式：jpeg/png/gif/webp（webp 解码来自 x/image）
func GenerateJPEG(srcPath, dstPath string, maxEdge, quality int) error {
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("读取原图失败: %w", err)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnsupportedImage, err)
	}

	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w <= 0 || h <= 0 {
		return ErrUnsupportedImage
	}
	// 已不超过目标尺寸：仍转 JPEG（统一缩略图格式），但不缩放
	scale := 1.0
	if m := max(w, h); m > maxEdge {
		scale = float64(maxEdge) / float64(m)
	}
	dw, dh := max(1, int(float64(w)*scale)), max(1, int(float64(h)*scale))

	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, bounds, draw.Src, nil)

	if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
		return fmt.Errorf("创建缩略图目录失败: %w", err)
	}
	out, err := os.Create(dstPath)
	if err != nil {
		return fmt.Errorf("创建缩略图文件失败: %w", err)
	}
	defer out.Close()
	if err := jpeg.Encode(out, dst, &jpeg.Options{Quality: quality}); err != nil {
		return fmt.Errorf("编码缩略图失败: %w", err)
	}
	return nil
}

// IsSupported 判断扩展名是否为可生成缩略图的图片
func IsSupported(ext string) bool {
	switch normalizeExt(ext) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp":
		return true
	}
	return false
}

func normalizeExt(ext string) string {
	e := filepath.Ext(ext)
	if e == "" && len(ext) > 0 && ext[0] != '.' {
		// 传入的可能是纯后缀（如 "jpg"）
		e = "." + ext
	}
	b := []byte(e)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 32
		}
	}
	return string(b)
}
