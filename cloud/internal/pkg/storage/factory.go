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
		// P2 阶段暂不实现 MinIO，预留接口
		return nil, fmt.Errorf("MinIO 驱动暂未实现，请使用 local 驱动")
	default:
		return nil, fmt.Errorf("不支持的存储驱动: %s", cfg.Driver)
	}
}
