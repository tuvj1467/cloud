// Package cleanup 提供过期数据清理任务
// cleanup.go：分片上传遗留清理（孤儿 .tmp 物理目录 + 过期 pending 分片记录）
package cleanup

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"

	"onecloud/ent"
	"onecloud/ent/filepart"
	"onecloud/ent/shareaccesslog"
	"onecloud/internal/pkg/cache"
	"onecloud/internal/pkg/storage"
	"onecloud/internal/repository"
)

const (
	// staleTmpAge 物理分片临时目录的过期阈值。
	// 取 Redis 分片元数据 TTL（24h，随每次 AddPart 续期）的 2 倍：
	// 若与 TTL 同为 24h，扫描周期（12h）会先于 Redis 过期清掉物理分片，
	// 造成「Redis 显示已传、磁盘已清」的重试死锁
	staleTmpAge = 48 * time.Hour
	// stalePendingAge pending 分片记录的过期阈值（含上传窗口余量）
	stalePendingAge = 48 * time.Hour
	// staleDeletedAge 回收站软删文件的保留期，超期每日自动彻底删除
	staleDeletedAge = 30 * 24 * time.Hour
	// accessLogRetention 分享访问审计日志保留期（防无界增长）
	accessLogRetention = 90 * 24 * time.Hour
	// sweepInterval 清理周期
	sweepInterval = 12 * time.Hour
)

// Start 启动清理任务：启动后 30 秒首扫，此后每 12 小时一次，随 ctx 取消退出
// .tmp/pending 清理仅对本地驱动生效（本地驱动约定；MinIO 侧建议对象生命周期规则）；
// storageDriver 用于回收站超期文件的物理删除
func Start(ctx context.Context, entClient *ent.Client, localRootDir string, storageDriver storage.Driver, rdb *redis.Client) {
	if entClient == nil || localRootDir == "" {
		return
	}
	go func() {
		select {
		case <-time.After(30 * time.Second):
		case <-ctx.Done():
			return
		}
		sweep(ctx, entClient, localRootDir, storageDriver, rdb)
		ticker := time.NewTicker(sweepInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				sweep(ctx, entClient, localRootDir, storageDriver, rdb)
			case <-ctx.Done():
				return
			}
		}
	}()
}

// sweep 执行一轮清理，各项失败仅记日志不互相阻塞
func sweep(ctx context.Context, entClient *ent.Client, localRootDir string, storageDriver storage.Driver, rdb *redis.Client) {
	purgeStaleDeleted(ctx, entClient, storageDriver, rdb)
	purgeStaleAccessLogs(ctx, entClient)
	if n, err := cleanTmpDirs(ctx, rdb, localRootDir, staleTmpAge); err != nil {
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

// purgeStaleDeleted 彻底删除软删超 30 天的文件（回收站自动清空）：
// 逐个复用 DeleteFull 事务（级联分享 + 归还配额），事务后清理 Redis 与物理文件
func purgeStaleDeleted(ctx context.Context, entClient *ent.Client, storageDriver storage.Driver, rdb *redis.Client) {
	if storageDriver == nil {
		return
	}
	before := time.Now().Add(-staleDeletedAge)
	fileRepo := repository.NewFileRepository(entClient)
	shareRepo := repository.NewFileShareRepository(entClient)
	files, err := fileRepo.ListStaleDeleted(ctx, before, 200)
	if err != nil {
		logx.Errorf("[cleanup] 查询超期回收站文件失败: %v", err)
		return
	}
	if len(files) == 0 {
		return
	}
	shareCache := cache.NewShareCache(rdb)
	md5Cache := cache.NewMD5Cache(rdb)
	purged := 0
	for _, f := range files {
		shares, err := shareRepo.ListByFileIDs(ctx, []string{f.ID})
		if err != nil {
			logx.Errorf("[cleanup] 查询分享失败 file=%s: %v", f.ID, err)
		}
		if err := fileRepo.DeleteFull(ctx, f); err != nil {
			logx.Errorf("[cleanup] 彻底删除失败 file=%s: %v", f.ID, err)
			continue
		}
		for _, sh := range shares {
			_ = shareCache.Del(ctx, sh.ShareCode)
		}
		if f.Md5 != "" {
			_ = md5Cache.Del(ctx, f.UserID, f.Md5, f.ID)
		}
		// 共享路径守卫：秒传允许跨记录共享物理文件，仍有引用时跳过物理删除
		if err := fileRepo.DeletePhysicalIfUnshared(ctx, storageDriver, f.Path); err != nil {
			logx.Errorf("[cleanup] 物理文件删除失败 file=%s path=%s: %v", f.ID, f.Path, err)
			continue
		}
		purged++
	}
	logx.Infof("[cleanup] 已彻底删除软删超 30 天文件 %d 个", purged)
}

// purgeStaleAccessLogs 删除超保留期的分享访问审计日志
func purgeStaleAccessLogs(ctx context.Context, entClient *ent.Client) {
	deadline := time.Now().Add(-accessLogRetention)
	n, err := entClient.ShareAccessLog.Delete().
		Where(shareaccesslog.CreatedAtLT(deadline)).
		Exec(ctx)
	if err != nil {
		logx.Errorf("[cleanup] 清理过期分享审计日志失败: %v", err)
		return
	}
	if n > 0 {
		logx.Infof("[cleanup] 已清理过期分享审计日志 %d 条", n)
	}
}

// cleanTmpDirs 删除 .tmp 下修改时间早于 age 的 uploadID 目录，返回删除数量
// 目录删除的同时清理对应的 Redis 上传状态（meta/parts），避免「Redis 显示已传、磁盘已清」
// 的状态分叉（断点续传命中 Redis 却无物理分片可合并）
func cleanTmpDirs(ctx context.Context, rdb *redis.Client, rootDir string, age time.Duration) (int, error) {
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
			// 同步清理 Redis 状态，保持与物理目录一致
			if rdb != nil {
				mpCache := cache.NewMultipartCache(rdb)
				_ = mpCache.Clear(ctx, e.Name())
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
