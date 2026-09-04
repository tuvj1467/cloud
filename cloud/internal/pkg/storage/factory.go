package storage

import (
	"fmt"
	"sync/atomic"
	"time"
)

// uploadIDSeq 用于生成本地分片上传的 upload_id（进程内递增）
var uploadIDSeq int64

// generateUploadID 生成一个递增的 upload_id（基于时间戳 + 序列号）
func generateUploadID() int64 {
	seq := atomic.AddInt64(&uploadIDSeq, 1)
	return time.Now().UnixNano() + seq
}

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
