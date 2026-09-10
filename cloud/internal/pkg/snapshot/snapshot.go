// Package snapshot 提供用量每日快照的补齐、定时聚合与趋势查询
package snapshot

import (
	"context"
	"time"

	"github.com/zeromicro/go-zero/core/logx"

	"onecloud/ent"
	"onecloud/ent/file"
	"onecloud/ent/storagesnapshot"
	"onecloud/internal/pkg/snowId"
)

const (
	backfillCap    = 90 * 24 * time.Hour // 最多回溯 90 天
	snapshotLayout = "2006-01-02"
)

// TrendPoint 趋势数据点
type TrendPoint struct {
	Date      string `json:"date"`
	FileCount int64  `json:"fileCount"`
	TotalSize int64  `json:"totalSize"`
}

// Start 启动快照任务：启动后 20s 做一次历史补齐（幂等），此后每日 00:05 聚合昨日快照
func Start(ctx context.Context, client *ent.Client, snowflake *snowId.Snowflake) {
	if client == nil || snowflake == nil {
		return
	}
	go func() {
		select {
		case <-time.After(20 * time.Second):
		case <-ctx.Done():
			return
		}
		if err := Backfill(ctx, client, snowflake); err != nil {
			logx.Errorf("[snapshot] 历史快照补齐失败: %v", err)
		}
		for {
			waitUntilNext0005(ctx)
			if ctx.Err() != nil {
				return
			}
			yesterday := time.Now().AddDate(0, 0, -1)
			if _, err := SnapshotForDate(ctx, client, snowflake, yesterday); err != nil {
				logx.Errorf("[snapshot] 每日快照失败 %s: %v", yesterday.Format(snapshotLayout), err)
			} else {
				logx.Infof("[snapshot] 每日快照完成 %s", yesterday.Format(snapshotLayout))
			}
		}
	}()
}

// waitUntilNext0005 阻塞至下一个 00:05（或 ctx 取消）
func waitUntilNext0005(ctx context.Context) {
	now := time.Now()
	next := time.Date(now.Year(), now.Month(), now.Day(), 0, 5, 0, 0, now.Location())
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	select {
	case <-time.After(time.Until(next)):
	case <-ctx.Done():
	}
}

// Backfill 补齐「最早文件日期（上限 90 天）→ 昨天」所有缺失的用户快照（幂等）
func Backfill(ctx context.Context, client *ent.Client, snowflake *snowId.Snowflake) error {
	var earliest struct {
		Min int64 `json:"min"`
	}
	// created_at 为 datetime，Min 聚合经 unix 时间戳取值（ent 对 time 聚合返回扫描不便，改用 SQL 时间函数）
	rows := []struct {
		Min string `json:"min"`
	}{}
	err := client.File.Query().
		Aggregate(ent.Min(file.FieldCreatedAt)).
		Scan(ctx, &rows)
	if err != nil {
		return err
	}
	if len(rows) == 0 || rows[0].Min == "" {
		return nil // 无文件，无需补齐
	}
	_ = earliest

	t, err := time.ParseInLocation("2006-01-02 15:04:05", rows[0].Min, time.Local)
	if err != nil {
		return err
	}
	loc := t.Location()
	start := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
	if cap := time.Now().Add(-backfillCap); start.Before(cap) {
		start = cap
	}

	done := 0
	for d := start; d.Before(time.Now()); d = d.AddDate(0, 0, 1) {
		n, err := SnapshotForDate(ctx, client, snowflake, d)
		if err != nil {
			return err
		}
		done += n
	}
	logx.Infof("[snapshot] 历史快照补齐完成，新增 %d 条", done)
	return nil
}

// SnapshotForDate 聚合「截至 date 当日末」各用户的累计文件数与总大小并写入快照
// 已存在的 (date, user_id) 快照跳过（幂等），返回新增条数
func SnapshotForDate(ctx context.Context, client *ent.Client, snowflake *snowId.Snowflake, d time.Time) (int, error) {
	dateStr := d.Format(snapshotLayout)
	end := d.AddDate(0, 0, 1)

	var rows []struct {
		UserID string `json:"user_id"`
		Count  int64  `json:"count"`
		Sum    int64  `json:"sum"`
	}
	err := client.File.Query().
		Where(file.CreatedAtLT(end)).
		GroupBy(file.FieldUserID).
		Aggregate(ent.Count(), ent.Sum(file.FieldSize)).
		Scan(ctx, &rows)
	if err != nil {
		return 0, err
	}

	created := 0
	for _, row := range rows {
		exists, err := client.StorageSnapshot.Query().
			Where(
				storagesnapshot.DateEQ(dateStr),
				storagesnapshot.UserIDEQ(row.UserID),
			).
			Exist(ctx)
		if err != nil {
			return created, err
		}
		if exists {
			continue
		}
		err = client.StorageSnapshot.Create().
			SetID(snowflake.MustIdS()).
			SetDate(dateStr).
			SetUserID(row.UserID).
			SetFileCount(row.Count).
			SetTotalSize(row.Sum).
			Exec(ctx)
		if err != nil {
			return created, err
		}
		created++
	}
	return created, nil
}

// Trend 用户用量趋势：自 start 日起（含）逐日数据点；快照缺失日补零，当日为实时聚合
func Trend(ctx context.Context, client *ent.Client, userID string, days int) ([]TrendPoint, error) {
	if days <= 0 {
		days = 30
	}
	today := time.Now()
	start := today.AddDate(0, 0, -(days - 1))
	startStr := start.Format(snapshotLayout)

	snaps, err := client.StorageSnapshot.Query().
		Where(
			storagesnapshot.UserIDEQ(userID),
			storagesnapshot.DateGTE(startStr),
		).
		Order(storagesnapshot.ByDate()).
		All(ctx)
	if err != nil {
		return nil, err
	}
	byDate := map[string]TrendPoint{}
	for _, s := range snaps {
		byDate[s.Date] = TrendPoint{Date: s.Date, FileCount: s.FileCount, TotalSize: s.TotalSize}
	}

	// 当日实时聚合（快照要到次日 00:05 才落）
	todayStr := today.Format(snapshotLayout)
	var live []struct {
		UserID string `json:"user_id"`
		Count  int64  `json:"count"`
		Sum    int64  `json:"sum"`
	}
	err = client.File.Query().
		Where(file.UserIDEQ(userID)).
		GroupBy(file.FieldUserID).
		Aggregate(ent.Count(), ent.Sum(file.FieldSize)).
		Scan(ctx, &live)
	if err != nil {
		return nil, err
	}

	points := make([]TrendPoint, 0, days)
	for i := 0; i < days; i++ {
		d := start.AddDate(0, 0, i).Format(snapshotLayout)
		if d == todayStr {
			p := TrendPoint{Date: d}
			if len(live) > 0 {
				p.FileCount, p.TotalSize = live[0].Count, live[0].Sum
			}
			points = append(points, p)
			continue
		}
		if p, ok := byDate[d]; ok {
			points = append(points, p)
		} else {
			points = append(points, TrendPoint{Date: d})
		}
	}
	return points, nil
}
