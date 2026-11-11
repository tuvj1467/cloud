// reconcile.go：硬崩溃残留兜底（doc/17 §3.9 识别的两类残留，进程存活的 rollback 盖不住）
//  1. Complete 门闩（TryMarkMerging）占有后、合并完成前硬崩溃 → 分片卡 merged：
//     客户端重试 Complete 被「正在合并或已完成」拒绝（complete_multipart_upload_logic.go:114-115），
//     而 cleanStalePendingParts 只清 pending（cleanup.go:201-215）——merged 行无人回收成永久僵尸；
//     由 CleanStaleMergedParts 超龄整组清除兜底。
//  2. 配额扣减后、文件记录写入前硬崩溃 → buckets.used_size 与 files 实际聚合漂移
//     （进程内失败路径的补偿归还 complete_multipart_upload_logic.go:122-144 只覆盖进程存活场景）；
//     由 ReconcileBucketQuota 周期对账检出并告警（不自动改数）。
package cleanup

import (
	"context"
	"fmt"
	"time"

	"onecloud/ent"
	"onecloud/ent/file"
	"onecloud/ent/filepart"
)

// staleMergedAge merged 僵尸分片的清除阈值（2× Redis 分片元数据 TTL，与 pending 一致）。
// 门闩一次 UPDATE 将全组分片置 merged（同一 updated_at 时间戳），正常合并秒级完成，
// 48h 远大于合理耗时，不会误清进行中的合并。
const staleMergedAge = 48 * time.Hour

// CleanStaleMergedParts 清理超龄 merged 僵尸分片：status=merged 且 updated_at 早于 age 的
// 分片按 upload_id 整组删除（先 GroupBy 定位僵尸组，再整组删除，返回删除的分片条数）。
//
// 语义取舍（为什么是删而不是恢复）：正常完成的 merged 记录本无查询方——Complete 成功后
// 上传语义已终结，File 记录与物理文件是唯一权威数据，分片行纯属留存；超龄清除使该上传
// 任务从「已完成」退化为「任务不存在」，与 pending 清理后的语义一致，可接受。唯一代价是
// 极迟到的客户端重试 Complete 得到「上传任务不存在或已过期」——它本就需要重新 init 全量
// 重传，与门闩拒绝后的处置路径相同。选择整组删除而非 RestoreUploaded 恢复：恢复会让
// Redis 元数据（24h 已过期）与 DB 状态重新分叉，重试合并依赖的 Redis 分片 MD5 清单已丢，
// 恢复后必然合并失败，徒增一轮无效重试。
func CleanStaleMergedParts(ctx context.Context, client *ent.Client, age time.Duration) (int, error) {
	deadline := time.Now().Add(-age)
	// 先按组定位僵尸 upload_id（GroupBy 去重，避免全量 merged 行逐条处理）
	var groups []struct {
		UploadID string `json:"upload_id"`
	}
	err := client.FilePart.Query().
		Where(
			filepart.StatusEQ(filepart.StatusMerged),
			filepart.UpdatedAtLT(deadline),
		).
		GroupBy(filepart.FieldUploadID).
		Scan(ctx, &groups)
	if err != nil {
		return 0, fmt.Errorf("查询超龄 merged 分片组失败: %w", err)
	}
	removed := 0
	for _, g := range groups {
		n, err := client.FilePart.Delete().
			Where(filepart.UploadIDEQ(g.UploadID)).
			Exec(ctx)
		if err != nil {
			return removed, fmt.Errorf("删除僵尸分片组失败 upload_id=%s: %w", g.UploadID, err)
		}
		removed += n
	}
	return removed, nil
}

// QuotaDrift 桶配额账实差异明细（一条对应一个 (user_id, bucket_id) 组）
type QuotaDrift struct {
	UserID    string `json:"user_id"`
	BucketID  string `json:"bucket_id"`
	FileCount int64  `json:"file_count"` // files 实际聚合条数（deleted_at IS NULL）
	FileSize  int64  `json:"file_size"`  // files 实际聚合 sum(size)
	UsedSize  int64  `json:"used_size"`  // buckets.used_size 记账值
	HasBucket bool   `json:"has_bucket"` // false=聚合组找不到对应桶记录（孤儿文件组）
}

// ReconcileBucketQuota 桶配额对账：按 files（GROUP BY user_id+bucket_id，deleted_at IS NULL）
// 聚合 count/sum(size)，与 buckets.used_size 逐桶比对，返回全部差异明细（len==0 即账实一致）。
//
// 只读不改：调用方（sweep）仅记告警日志，不自动改数——配额是记账数据，漂移的成因
// （崩溃窗口位置、丢失的是扣减还是归还）无法从聚合值反推，盲改会把「可定位的漂移」
// 变成「不可追溯的错误对平」；生产处置流程：按明细核对操作日志后人工修正。
func ReconcileBucketQuota(ctx context.Context, client *ent.Client) ([]QuotaDrift, error) {
	var rows []struct {
		UserID   string `json:"user_id"`
		BucketID string `json:"bucket_id"`
		Count    int64  `json:"count"`
		Sum      int64  `json:"sum"`
	}
	err := client.File.Query().
		Where(file.DeletedAtIsNil()).
		GroupBy(file.FieldUserID, file.FieldBucketID).
		Aggregate(ent.Count(), ent.Sum(file.FieldSize)).
		Scan(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("聚合 files 配额失败: %w", err)
	}

	type aggKey struct{ user, bkt string }
	agg := make(map[aggKey]QuotaDrift, len(rows))
	for _, r := range rows {
		agg[aggKey{r.UserID, r.BucketID}] = QuotaDrift{
			UserID:    r.UserID,
			BucketID:  r.BucketID,
			FileCount: r.Count,
			FileSize:  r.Sum,
		}
	}

	buckets, err := client.Bucket.Query().All(ctx)
	if err != nil {
		return nil, fmt.Errorf("查询桶列表失败: %w", err)
	}
	var drifts []QuotaDrift
	for _, b := range buckets {
		g, ok := agg[aggKey{b.UserID, b.ID}]
		if !ok {
			g = QuotaDrift{UserID: b.UserID, BucketID: b.ID}
		}
		g.HasBucket = true
		if g.FileSize != b.UsedSize {
			g.UsedSize = b.UsedSize
			drifts = append(drifts, g)
		}
		delete(agg, aggKey{b.UserID, b.ID})
	}
	// 剩余聚合组：文件存在而桶记录缺失（如桶被绕过 DB 删除），同属漂移
	for _, g := range agg {
		g.UsedSize = 0
		drifts = append(drifts, g)
	}
	return drifts, nil
}
