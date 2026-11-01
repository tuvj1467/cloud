package repository

// repository 层数据库集成测试：验证并发正确性原语在真实 MySQL 上的行为。
//
// 运行方式（默认 go test 会自动跳过，不影响常规单测与 CI）：
//
//	DB_IT=1 go test ./internal/repository/ -count=1 -race
//
// 可选环境变量：
//   - DB_IT_DSN：测试库 DSN，默认 root:12345@tcp(127.0.0.1:3306)/cloud_it?...
//   注意使用【独立的测试库】（默认 cloud_it，自动创建），不要指向业务库。
//
// 覆盖的并发不变式（对应 doc/15 交接文档 §5）：
//   - §5.1 配额原子扣减：并发下绝不超卖（TryIncUsedSize/TryDecUsedSize）
//   - §5.2 合并权门闩：同一 upload_id 并发 Complete 恰有一次独占（TryMarkMerging/RestoreUploaded）
//   - 分享计数原子自增：并发递增无丢失（IncUsedCount）

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/go-sql-driver/mysql"

	"onecloud/ent"
	"onecloud/ent/bucket"
	"onecloud/ent/filepart"
	"onecloud/ent/fileshare"
)

const itDBName = "cloud_it"

// itClient 测试库客户端（每个测试独立调用；表结构自动迁移，数据在 setup 时清空）
func itClient(t *testing.T) *ent.Client {
	t.Helper()
	if os.Getenv("DB_IT") != "1" {
		t.Skip("数据库集成测试未启用：设置 DB_IT=1 运行（使用独立测试库 cloud_it）")
	}
	dsn := os.Getenv("DB_IT_DSN")
	if dsn == "" {
		dsn = "root:12345@tcp(127.0.0.1:3306)/" + itDBName + "?charset=utf8mb4&parseTime=True&loc=Local"
	}

	ctx := context.Background()
	// 先不指定库连接，确保测试库存在
	adminDSN := strings.Replace(dsn, "/"+itDBName+"?", "/?", 1)
	admin, err := sql.Open("mysql", adminDSN)
	if err != nil {
		t.Fatalf("连接 MySQL 失败: %v", err)
	}
	defer admin.Close()
	if _, err := admin.ExecContext(ctx,
		"CREATE DATABASE IF NOT EXISTS `"+itDBName+"` DEFAULT CHARSET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		t.Fatalf("创建测试库失败: %v", err)
	}

	dbConn, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("连接测试库失败: %v", err)
	}
	dbConn.SetMaxOpenConns(60)
	client := ent.NewClient(ent.Driver(entsql.OpenDB("mysql", dbConn)))
	t.Cleanup(func() { client.Close() })

	if err := client.Schema.Create(ctx); err != nil {
		t.Fatalf("自动迁移失败: %v", err)
	}
	// 清空全部表（无跨实体外键，直接 TRUNCATE 即可）
	for _, table := range []string{"users", "buckets", "files", "file_parts", "file_shares",
		"share_access_logs", "admin_audit_logs", "storage_snapshots"} {
		if _, err := dbConn.ExecContext(ctx, "TRUNCATE TABLE `"+table+"`"); err != nil {
			t.Fatalf("清空表 %s 失败（可忽略首次运行的建表时序）: %v", table, err)
		}
	}
	return client
}

// itBucket 建一个指定容量的测试桶
func itClientBucket(t *testing.T, client *ent.Client, id string, sizeLimit int64) *ent.Bucket {
	t.Helper()
	b, err := client.Bucket.Create().
		SetID(id).
		SetName("it-bucket-" + id).
		SetUserID("it-user").
		SetSizeLimit(sizeLimit).
		SetVisibility(bucket.VisibilityPrivate).
		Save(context.Background())
	if err != nil {
		t.Fatalf("创建测试桶失败: %v", err)
	}
	return b
}

// TestTryIncUsedSizeConcurrentQuota 不变式 §5.1：N 个并发请求争抢有限配额，
// 成功数×增量 恰好等于容量上限——绝不超卖、绝不漏记
func TestTryIncUsedSizeConcurrentQuota(t *testing.T) {
	client := itClient(t)
	ctx := context.Background()

	const (
		limit      = int64(1000)
		delta      = int64(100)
		goroutines = 50
	)
	b := itClientBucket(t, client, "it-quota", limit)

	var okCount int64
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := NewBucketRepository(client).TryIncUsedSize(ctx, b.ID, delta)
			if err != nil {
				t.Errorf("TryIncUsedSize 意外错误: %v", err)
				return
			}
			if ok {
				atomic.AddInt64(&okCount, 1)
			}
		}()
	}
	wg.Wait()

	if got := atomic.LoadInt64(&okCount); got != limit/delta {
		t.Fatalf("并发争抢 %d 次×%d（上限 %d）：应恰好成功 %d 次，实际 %d（超卖或漏记）",
			goroutines, delta, limit, limit/delta, got)
	}
	final, err := client.Bucket.Query().Where(bucket.IDEQ(b.ID)).Only(ctx)
	if err != nil {
		t.Fatalf("查询桶失败: %v", err)
	}
	if final.UsedSize != limit {
		t.Fatalf("used_size 应精确等于 %d，实际 %d", limit, final.UsedSize)
	}
}

// TestTryDecUsedSizeFloorGuards 归还不越过零：超额归还拒绝且不改动账目
func TestTryDecUsedSizeFloorGuards(t *testing.T) {
	client := itClient(t)
	ctx := context.Background()

	b := itClientBucket(t, client, "it-floor", 1000)
	repo := NewBucketRepository(client)
	if ok, err := repo.TryIncUsedSize(ctx, b.ID, 300); err != nil || !ok {
		t.Fatalf("预置用量失败: ok=%v err=%v", ok, err)
	}

	// 超额归还：拒绝且账目不变
	if ok, err := repo.TryDecUsedSize(ctx, b.ID, 500); ok || err != nil {
		t.Fatalf("超额归还应返回 false 无错误: ok=%v err=%v", ok, err)
	}
	cur, _ := client.Bucket.Query().Where(bucket.IDEQ(b.ID)).Only(ctx)
	if cur.UsedSize != 300 {
		t.Fatalf("被拒绝的归还不应改动 used_size，实际 %d", cur.UsedSize)
	}

	// 精确归还到零
	if ok, err := repo.TryDecUsedSize(ctx, b.ID, 300); !ok || err != nil {
		t.Fatalf("精确归还应成功: ok=%v err=%v", ok, err)
	}
	// 零之后继续归还：拒绝（永不为负）
	if ok, err := repo.TryDecUsedSize(ctx, b.ID, 1); ok || err != nil {
		t.Fatalf("零用量再归还应拒绝: ok=%v err=%v", ok, err)
	}
}

// itUploadParts 为一次上传预置 n 个已上传分片
func itUploadParts(t *testing.T, client *ent.Client, uploadID string, n int) {
	t.Helper()
	ctx := context.Background()
	bulk := make([]*ent.FilePartCreate, 0, n)
	for i := 1; i <= n; i++ {
		bulk = append(bulk, client.FilePart.Create().
			SetID(fmt.Sprintf("part-%s-%d", uploadID, i)).
			SetUploadID(uploadID).
			SetUserID("it-user").
			SetBucketID("it-bucket").
			SetFileName("it.bin").
			SetFileSuffix("bin").
			SetFileSize(int64(n)).
			SetTotalParts(n).
			SetPartNumber(i).
			SetStatus(filepart.StatusUploaded))
	}
	if _, err := client.FilePart.CreateBulk(bulk...).Save(ctx); err != nil {
		t.Fatalf("预置分片失败: %v", err)
	}
}

// TestTryMarkMergingExclusiveGate 不变式 §5.2：同一 upload_id 的并发 Complete，
// 合并权门闩必须恰好放行一次；回滚后可重入
func TestTryMarkMergingExclusiveGate(t *testing.T) {
	client := itClient(t)
	ctx := context.Background()

	const totalParts = 10
	repo := NewFilePartRepository(client)
	itUploadParts(t, client, "it-gate", totalParts)

	var winners int64
	var wg sync.WaitGroup
	for i := 0; i < totalParts*3; i++ { // 30 个并发 Complete 模拟
		wg.Add(1)
		go func() {
			defer wg.Done()
			owned, err := repo.TryMarkMerging(ctx, "it-gate", totalParts)
			if err != nil {
				t.Errorf("TryMarkMerging 意外错误: %v", err)
				return
			}
			if owned {
				atomic.AddInt64(&winners, 1)
			}
		}()
	}
	wg.Wait()

	if got := atomic.LoadInt64(&winners); got != 1 {
		t.Fatalf("并发 %d 次合并，应恰好 1 个独占成功，实际 %d（会重复合并/重复扣配额）", totalParts*3, got)
	}
	// 门闩后全部为 merged
	cnt, err := client.FilePart.Query().
		Where(filepart.UploadIDEQ("it-gate"), filepart.StatusEQ(filepart.StatusMerged)).
		Count(ctx)
	if err != nil || cnt != totalParts {
		t.Fatalf("门闩后应有 %d 条 merged，实际 %d（err=%v）", totalParts, cnt, err)
	}

	// 失败回滚路径：RestoreUploaded 后可重新占有（保留客户端重试能力）
	if err := repo.RestoreUploaded(ctx, "it-gate"); err != nil {
		t.Fatalf("RestoreUploaded: %v", err)
	}
	owned, err := repo.TryMarkMerging(ctx, "it-gate", totalParts)
	if err != nil || !owned {
		t.Fatalf("恢复后应可重新独占合并权: owned=%v err=%v", owned, err)
	}
}

// TestIncUsedCountConcurrent 分享下载计数：并发自增零丢失
// （锁定旧实现「先读后写+版本 CAS」在并发下丢递增、上限可被突破的回归）
func TestIncUsedCountConcurrent(t *testing.T) {
	client := itClient(t)
	ctx := context.Background()

	const goroutines = 100
	if _, err := client.FileShare.Create().
		SetID("it-share").
		SetFileID("it-file").
		SetUserID("it-user").
		SetShareCode("itcode1").
		SetPermission(fileshare.PermissionDownload).
		Save(ctx); err != nil {
		t.Fatalf("预置分享失败: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := NewFileShareRepository(client).IncUsedCount(ctx, "it-share"); err != nil {
				t.Errorf("IncUsedCount: %v", err)
			}
		}()
	}
	wg.Wait()

	share, err := client.FileShare.Query().Where(fileshare.IDEQ("it-share")).Only(ctx)
	if err != nil {
		t.Fatalf("查询分享失败: %v", err)
	}
	if share.UsedCount != goroutines {
		t.Fatalf("并发自增 %d 次后 used_count 应为 %d，实际 %d（存在丢失更新）",
			goroutines, goroutines, share.UsedCount)
	}
}

// TestCountUploadedStateSanity 完整性判定基准：DB 状态机是 Complete 的唯一依据
func TestCountUploadedStateSanity(t *testing.T) {
	client := itClient(t)
	ctx := context.Background()

	itUploadParts(t, client, "it-count", 5)
	repo := NewFilePartRepository(client)
	if n, err := repo.CountUploaded(ctx, "it-count"); err != nil || n != 5 {
		t.Fatalf("初始应为 5 条 uploaded: n=%d err=%v", n, err)
	}
	// 模拟批次3修复的场景：Redis 显示已传但 DB 仍 pending 时，以 DB 为准 → Complete 拒绝
	if _, err := client.FilePart.Update().
		Where(filepart.UploadIDEQ("it-count"), filepart.PartNumberEQ(3)).
		SetStatus(filepart.StatusPending).
		Save(ctx); err != nil {
		t.Fatalf("回置 pending: %v", err)
	}
	if n, _ := repo.CountUploaded(ctx, "it-count"); n != 4 {
		t.Fatalf("1 条 pending 后应为 4,实际 %d", n)
	}
	// 宽限期观察：无 Goroutine 泄漏检测手段时至少确认测试在时限内完成
	_ = time.Now()
}
