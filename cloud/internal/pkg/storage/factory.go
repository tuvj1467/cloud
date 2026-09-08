package storage

import (
	"fmt"
)

// NewDriver 根据配置返回存储驱动实例
func NewDriver(cfg Config) (Driver, error) {
	switch cfg.Driver {
	case "local", "":
		rootDir := cfg.Local.RootDir
		if rootDir == "" {
			rootDir = "./cloud_storage"
		}
		return NewLocalDriver(rootDir), nil
	case "minio":
		// MinIO / S3 兼容驱动（P10 实现；运行验证需接真实 MinIO 服务）
		return NewMinioDriver(cfg.Minio)
	default:
		return nil, fmt.Errorf("不支持的存储驱动: %s", cfg.Driver)
	}
}
