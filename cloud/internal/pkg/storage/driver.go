// Package storage 提供对象存储驱动抽象层
// 通过统一的 Driver 接口隔离物理存储实现（本地磁盘 / MinIO / S3）
package storage

import (
	"context"
	"errors"
	"io"
)

// Config 存储驱动配置（由 config.Config 引用）
type Config struct {
	Driver string `json:",default=local"` // local | minio | s3
	Local  LocalConfig
	Minio  MinioConfig
}

// LocalConfig 本地磁盘存储配置
type LocalConfig struct {
	RootDir string `json:",default=./cloud_storage"` // 存储根目录
}

// MinioConfig MinIO / S3 兼容存储配置（P2 阶段暂不实现，预留接口）
type MinioConfig struct {
	Endpoint  string `json:",optional"`
	AccessKey string `json:",optional"`
	SecretKey string `json:",optional"`
	Bucket    string `json:",optional"`
	UseSSL    bool   `json:",optional"`
}

// FileInfo 存储层文件信息
type FileInfo struct {
	Size         int64  // 文件大小（字节）
	LastModified int64  // 最后修改时间（unix 秒）
	ContentType  string // MIME 类型
}

// ErrNotImplemented 当前驱动不支持该操作
var ErrNotImplemented = errors.New("当前存储驱动不支持该操作")

// ErrFileNotFound 文件不存在
var ErrFileNotFound = errors.New("文件不存在")

// Driver 存储驱动接口，所有后端必须实现
type Driver interface {
	// Put 上传完整文件（小文件）
	// path: 相对路径（如 user_id/20260727/file_id.jpg）
	Put(ctx context.Context, path string, reader io.Reader, size int64) error

	// Get 下载文件，返回可读流（调用方负责 Close）
	Get(ctx context.Context, path string) (io.ReadCloser, error)

	// Delete 删除文件
	Delete(ctx context.Context, path string) error

	// Stat 获取文件信息（大小、修改时间）
	Stat(ctx context.Context, path string) (*FileInfo, error)

	// Exists 判断文件是否存在
	Exists(ctx context.Context, path string) (bool, error)

	// InitPart 初始化分片上传，返回 upload_id
	InitPart(ctx context.Context, path string, totalParts int) (uploadID string, err error)

	// UploadPart 上传单个分片
	UploadPart(ctx context.Context, uploadID string, partNumber int, reader io.Reader, size int64) (partMD5 string, err error)

	// CompletePart 合并所有分片为完整文件
	CompletePart(ctx context.Context, uploadID string, targetPath string) error

	// AbortPart 取消分片上传，清理临时分片
	AbortPart(ctx context.Context, uploadID string) error

	// PresignURL 生成预签名 URL（直传/直下，MinIO/S3 适用）
	// 本地驱动返回 ErrNotImplemented
	PresignURL(ctx context.Context, path string, expire int64, method string) (string, error)
}
