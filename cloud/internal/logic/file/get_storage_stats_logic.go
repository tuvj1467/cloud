package filelogic

import (
	"context"
	"errors"

	"onecloud/ent"
	"onecloud/ent/bucket"
	"onecloud/ent/file"
	"onecloud/internal/pkg/jwt"
	"onecloud/internal/pkg/response"
	"onecloud/internal/svc"
	"onecloud/proto/.proto/service"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetStorageStatsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetStorageStatsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetStorageStatsLogic {
	return &GetStorageStatsLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// GetStorageStats 用量统计：用户文件总数 / 实际总大小 / 桶数量 + 分桶明细
// 实现取舍：桶数量小，逐桶 Count + Sum 而非一次 GroupBy——SQL 简单，且
// 避免 SUM 空集返回 NULL 的扫描歧义（仅在 count>0 时才求和）
func (l *GetStorageStatsLogic) GetStorageStats(in *service.GetStorageStatsRequest) (*service.Response, error) {
	if err := in.Validate(); err != nil {
		return response.Err(errors.New("参数校验失败"))
	}

	userID, err := jwt.GetUserIdFromCtx(l.ctx)
	if err != nil {
		return response.Err(err)
	}

	client := l.svcCtx.EntClient

	// 1. 一次 GROUP BY 拿到用户级与分桶级聚合（原实现：用户级 2 查 + 逐桶 2 查的 N+1）
	//    GROUP BY 只产出 count>0 的分组，天然规避 SUM()=NULL 的空集歧义
	var rows []struct {
		BucketID string `json:"bucket_id"`
		Count    int64  `json:"count"`
		Sum      int64  `json:"sum"`
	}
	if err := client.File.Query().
		Where(file.UserIDEQ(userID), file.DeletedAtIsNil()).
		GroupBy(file.FieldBucketID).
		Aggregate(ent.Count(), ent.Sum(file.FieldSize)).
		Scan(l.ctx, &rows); err != nil {
		return response.Err(errors.New("统计文件用量失败: " + err.Error()))
	}
	var fileCount, totalSize int64
	perBucket := make(map[string][2]int64, len(rows))
	for _, r := range rows {
		fileCount += r.Count
		totalSize += r.Sum
		perBucket[r.BucketID] = [2]int64{r.Count, r.Sum}
	}

	// 2. 桶信息（name/used_size/size_limit）取自 buckets 表，与聚合结果内存拼装
	buckets, err := client.Bucket.Query().Where(bucket.UserIDEQ(userID)).All(l.ctx)
	if err != nil {
		return response.Err(errors.New("查询桶列表失败: " + err.Error()))
	}

	bucketStats := make([]*service.BucketStats, 0, len(buckets))
	for _, b := range buckets {
		stat := &service.BucketStats{
			BucketId:  b.ID,
			Name:      b.Name,
			UsedSize:  b.UsedSize,
			SizeLimit: b.SizeLimit,
		}
		if ag, ok := perBucket[b.ID]; ok {
			stat.FileCount = ag[0]
			stat.TotalSize = ag[1]
		}
		bucketStats = append(bucketStats, stat)
	}

	l.Infof("storage stats user=%s files=%d total_size=%d buckets=%d", userID, fileCount, totalSize, len(buckets))

	return response.Success(&service.StorageStatsResponse{
		FileCount:   int64(fileCount),
		TotalSize:   totalSize,
		BucketCount: int64(len(buckets)),
		Buckets:     bucketStats,
	})
}
