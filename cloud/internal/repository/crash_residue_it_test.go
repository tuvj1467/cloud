package repository_test

// 硬崩溃残留兜底的 DB_IT 用例（doc/17 §3.9，实现 internal/pkg/cleanup/reconcile.go）：
//   - cleanup.CleanStaleMergedParts：超龄 merged 僵尸整组清除，新鲜 merged 与超龄 pending 不受影响
//   - cleanup.ReconcileBucketQuota：账实一致返回空差异；漂移/孤儿组检出且明细正确
//
// 为何是外部测试包（repository_test）：cleanup 包 import repository，内部测试包
// （package repository）再 import cleanup 会构成测试导入环；外部包无此限制，
// 且仍由 `DB_IT=1 go test ./internal/repository/` 门控运行。itClient 基建复用 internal/itdb。

import (
	"context"
	"fmt"
	"testing"
	"time"

	"onecloud/ent"
	"onecloud/ent/file"
	"onecloud/ent/filepart"
	"onecloud/internal/itdb"
	"onecloud/internal/pkg/cleanup"
)

// itMergedParts 预置一次上传的 n 条指定状态分片，updated_at 显式回溯
// （模拟门闩 TryMarkMerging 置 merged 后进程硬崩溃、updated_at 停格）
func itMergedParts(t *testing.T, client *ent.Client, uploadID string, n int, status filepart.Status, updatedAt time.Time) {
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
			SetFileSize(int64(10)).
			SetTotalParts(n).
			SetPartNumber(i).
			SetStatus(status).
			SetUpdatedAt(updatedAt))
	}
	if _, err := client.FilePart.CreateBulk(bulk...).Save(ctx); err != nil {
		t.Fatalf("预置分片失败: %v", err)
	}
}

// itFile 预置一条文件记录（deletedAt 为 nil 表示正常，否则软删进回收站）
func itFile(t *testing.T, client *ent.Client, id, userID, bucketID string, size int64, deletedAt *time.Time) {
	t.Helper()
	ctx := context.Background()
	c := client.File.Create().
		SetID(id).
		SetName(id + ".bin").
		SetPath(userID + "/20260101/" + id + ".bin").
		SetSize(size).
		SetMd5(fmt.Sprintf("md5-%s", id)).
		SetContentType("application/octet-stream").
		SetBucketID(bucketID).
		SetUserID(userID)
	if deletedAt != nil {
		c.SetDeletedAt(*deletedAt)
	}
	if _, err := c.Save(ctx); err != nil {
		t.Fatalf("预置文件失败: %v", err)
	}
}

// TestCleanStaleMergedZombieParts 场景①：门闩占有后硬崩溃的 merged 僵尸，超龄整组清除；
// 新鲜 merged（正常完成留存）与超龄 pending（归既有 cleanStalePendingParts 管）不受影响
func TestCleanStaleMergedZombieParts(t *testing.T) {
	client := itdb.Client(t)
	ctx := context.Background()

	// 崩溃残留组：updated_at 停在 49h 前（超过 48h 阈值）
	itMergedParts(t, client, "it-zombie", 10, filepart.StatusMerged, time.Now().Add(-49*time.Hour))
	// 新鲜组：正常完成后的 merged 留存，1h 前置位
	itMergedParts(t, client, "it-fresh", 3, filepart.StatusMerged, time.Now().Add(-1*time.Hour))
	// 对照组：超龄但仍是 pending（应留给 cleanStalePendingParts,本函数不得误删）
	itMergedParts(t, client, "it-pending", 2, filepart.StatusPending, time.Now().Add(-49*time.Hour))

	removed, err := cleanup.CleanStaleMergedParts(ctx, client, 48*time.Hour)
	if err != nil {
		t.Fatalf("CleanStaleMergedParts: %v", err)
	}
	if removed != 10 {
		t.Fatalf("应恰好清除僵尸组 10 条分片，实际 %d", removed)
	}
	for _, tc := range []struct {
		uploadID string
		want     int
		what     string
	}{
		{"it-zombie", 0, "僵尸组应整组清除"},
		{"it-fresh", 3, "新鲜 merged 应保留"},
		{"it-pending", 2, "超龄 pending 不归本函数管,应保留"},
	} {
		cnt, err := client.FilePart.Query().Where(filepart.UploadIDEQ(tc.uploadID)).Count(ctx)
		if err != nil || cnt != tc.want {
			t.Fatalf("%s: upload=%s 期望 %d 条,实际 %d（err=%v）", tc.what, tc.uploadID, tc.want, cnt, err)
		}
	}

	// 幂等性：再跑一轮应无新增删除
	if n, err := cleanup.CleanStaleMergedParts(ctx, client, 48*time.Hour); err != nil || n != 0 {
		t.Fatalf("二次清理应为空: n=%d err=%v", n, err)
	}
}

// TestReconcileBucketQuotaDrift 场景②：账实一致返回空差异；
// 漂移（记账≠聚合）与孤儿文件组（桶记录缺失）检出且明细正确；软删文件不计入聚合
func TestReconcileBucketQuotaDrift(t *testing.T) {
	client := itdb.Client(t)
	ctx := context.Background()

	// —— 第一步：账实一致（含软删干扰项）→ 空差异 ——
	bOK := itdb.Bucket(t, client, "it-rc-ok", 1<<30)
	itFile(t, client, "f-rc-1", bOK.UserID, bOK.ID, 300, nil)
	itFile(t, client, "f-rc-2", bOK.UserID, bOK.ID, 200, nil)
	if _, err := client.Bucket.UpdateOneID(bOK.ID).SetUsedSize(500).Save(ctx); err != nil {
		t.Fatalf("设置记账值: %v", err)
	}
	// 软删进回收站的文件不计入聚合：即便 used_size 只对账 500 也应视为一致
	deletedAt := time.Now().Add(-time.Hour)
	itFile(t, client, "f-rc-deleted", bOK.UserID, bOK.ID, 700, &deletedAt)

	drifts, err := cleanup.ReconcileBucketQuota(ctx, client)
	if err != nil {
		t.Fatalf("ReconcileBucketQuota: %v", err)
	}
	if len(drifts) != 0 {
		t.Fatalf("账实一致应返回空差异，实际 %d 组: %+v", len(drifts), drifts)
	}

	// —— 第二步：注入漂移 → 检出且明细正确 ——
	// 漂移桶：记账 800，实际聚合 100
	bDrift := itdb.Bucket(t, client, "it-rc-drift", 1<<30)
	itFile(t, client, "f-rc-4", bDrift.UserID, bDrift.ID, 100, nil)
	if _, err := client.Bucket.UpdateOneID(bDrift.ID).SetUsedSize(800).Save(ctx); err != nil {
		t.Fatalf("设置记账值: %v", err)
	}
	// 孤儿文件组：files 有聚合、buckets 无对应记录
	itFile(t, client, "f-rc-ghost", "it-user", "it-rc-ghost", 50, nil)

	drifts, err = cleanup.ReconcileBucketQuota(ctx, client)
	if err != nil {
		t.Fatalf("ReconcileBucketQuota: %v", err)
	}
	if len(drifts) != 2 {
		t.Fatalf("应检出 2 组差异（漂移桶+孤儿组），实际 %d 组: %+v", len(drifts), drifts)
	}
	byBucket := make(map[string]cleanup.QuotaDrift, len(drifts))
	for _, d := range drifts {
		byBucket[d.BucketID] = d
	}
	if d, ok := byBucket["it-rc-drift"]; !ok {
		t.Fatalf("漂移桶未检出: %+v", drifts)
	} else if d.UserID != bDrift.UserID || d.FileCount != 1 || d.FileSize != 100 ||
		d.UsedSize != 800 || !d.HasBucket {
		t.Fatalf("漂移桶明细不符: %+v（期望 files=1 sum=100 used=800 has_bucket=true）", d)
	}
	if d, ok := byBucket["it-rc-ghost"]; !ok {
		t.Fatalf("孤儿文件组未检出: %+v", drifts)
	} else if d.HasBucket || d.UserID != "it-user" || d.FileCount != 1 || d.FileSize != 50 || d.UsedSize != 0 {
		t.Fatalf("孤儿组明细不符: %+v（期望 has_bucket=false files=1 sum=50）", d)
	}
	if _, ok := byBucket["it-rc-ok"]; ok {
		t.Fatalf("一致桶不应出现在差异明细中: %+v", drifts)
	}

	// —— 第三步：对账只读——不改任何数据 ——
	after, err := client.Bucket.Query().All(ctx)
	if err != nil {
		t.Fatalf("查询桶: %v", err)
	}
	for _, b := range after {
		if b.ID == "it-rc-drift" && b.UsedSize != 800 {
			t.Fatalf("对账不得自动改数: 漂移桶 used_size 被改为 %d", b.UsedSize)
		}
	}
	if cnt, _ := client.File.Query().Where(file.BucketIDEQ("it-rc-ghost")).Count(ctx); cnt != 1 {
		t.Fatalf("对账不得删除孤儿文件记录,实际 %d 条", cnt)
	}
}
