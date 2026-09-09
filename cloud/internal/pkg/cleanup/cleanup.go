// Package cleanup 提供过期数据清理任务
// cleanup.go：分片上传遗留清理（孤儿 .tmp 物理目录 + 过期 pending 分片记录）
package cleanup

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/zeromicro/go-zero/core/logx"

	"onecloud/ent"
	"onecloud/ent/filepart"
)

const (
	// staleTmpAge 物理分片临时目录的过期阈值（分片元数据 Redis TTL 24h 对齐）
	staleTmpAge = 24 * time.Hour
	// stalePendingAge pending 分片记录的过期阈值（含上传窗口余量）
	stalePendingAge = 48 * time.Hour
	// sweepInterval 清理周期
	sweepInterval = 12 * time.Hour
)

// Start 启动清理任务：启动后 30 秒首扫，此后每 12 小时一次，随 ctx 取消退出
// 仅对本地驱动生效（.tmp 为本地驱动约定）；MinIO 侧建议配置对象生命周期规则
func Start(ctx context.Context, entClient *ent.Client, localRootDir string) {
	if entClient == nil || localRootDir == "" {
		return
	}
	go func() {
		select {
		case <-time.After(30 * time.Second):
		case <-ctx.Done():
			return
		}
		sweep(ctx, entClient, localRootDir)
		ticker := time.NewTicker(sweepInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				sweep(ctx, entClient, localRootDir)
			case <-ctx.Done():
				return
			}
		}
	}()
}

// sweep 执行一轮清理，各项失败仅记日志不互相阻塞
func sweep(ctx context.Context, entClient *ent.Client, localRootDir string) {
	if n, err := cleanTmpDirs(localRootDir, staleTmpAge); err != nil {
		logx.Errorf("[cleanup] 清理孤儿 .tmp 目录失败: %v", err)
	} else if n > 0 {
		logx.Infof("[cleanup] 已清理过期 .tmp 分片目录 %d 个", n)
	}
	if n, err := cleanStalePendingParts(ctx, entClient, stalePendingAge); err != nil {
		logx.Errorf("[cleanup] 清理过期 pending 分片记录失败: %v", err)
	} else if n > 0 {
		logx.Infof("[cleanup] 已清理过期 pending 分片记录 %d 条", n)
	}
}

// cleanTmpDirs 删除 .tmp 下修改时间早于 age 的 uploadID 目录，返回删除数量
func cleanTmpDirs(rootDir string, age time.Duration) (int, error) {
	tmpDir := filepath.Join(rootDir, ".tmp")
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	deadline := time.Now().Add(-age)
	removed := 0
	for _, e := range entries {
		if !e.IsDir() || !validUploadDirName(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		// 目录 ModTime 不随分片文件写入而更新，逐文件取最新修改时间
		latest := info.ModTime()
		if subs, err := os.ReadDir(filepath.Join(tmpDir, e.Name())); err == nil {
			for _, sf := range subs {
				if sfi, err := sf.Info(); err == nil && sfi.ModTime().After(latest) {
					latest = sfi.ModTime()
				}
			}
		}
		if latest.Before(deadline) {
			if err := os.RemoveAll(filepath.Join(tmpDir, e.Name())); err != nil {
				logx.Errorf("[cleanup] 删除分片目录失败 upload_id=%s: %v", e.Name(), err)
				continue
			}
			removed++
		}
	}
	return removed, nil
}

// validUploadDirName 目录名须为雪花 ID 形态（防误删 .tmp 下手工放置的目录）
func validUploadDirName(name string) bool {
	if name == "" || len(name) > 32 {
		return false
	}
	for _, c := range name {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// cleanStalePendingParts 删除创建时间早于 age 的 pending 分片记录，返回删除数量
// （任务已放弃：Redis 元数据 24h 过期后 complete 本就不可能成功）
func cleanStalePendingParts(ctx context.Context, client *ent.Client, age time.Duration) (int, error) {
	deadline := time.Now().Add(-age)
	n, err := client.FilePart.Delete().
		Where(
			filepart.StatusEQ(filepart.StatusPending),
			filepart.CreatedAtLT(deadline),
		).
		Exec(ctx)
	if err != nil {
		return 0, err
	}
	return n, nil
}
