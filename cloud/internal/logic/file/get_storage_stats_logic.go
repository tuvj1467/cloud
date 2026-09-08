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

	// 1. 用户级聚合：文件总数 + 实际总大小
	fileCount, err := client.File.Query().Where(file.UserIDEQ(userID)).Count(l.ctx)
	if err != nil {
		return response.Err(errors.New("统计文件数失败: " + err.Error()))
	}

	var totalSize int64
	if fileCount > 0 { // 空用户跳过 Sum，规避 SUM()=NULL
		var sizes []int64
		if err := client.File.Query().Where(file.UserIDEQ(userID)).
			Aggregate(ent.Sum(file.FieldSize)).Scan(l.ctx, &sizes); err != nil {
			return response.Err(errors.New("统计文件大小失败: " + err.Error()))
		}
		if len(sizes) > 0 {
			totalSize = sizes[0]
		}
	}

	// 2. 分桶聚合：桶信息（name/used_size/size_limit）取自 buckets 表
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

		cnt, err := client.File.Query().Where(file.UserIDEQ(userID), file.BucketIDEQ(b.ID)).Count(l.ctx)
		if err != nil {
			return response.Err(errors.New("统计桶内文件数失败: " + err.Error()))
		}
		stat.FileCount = int64(cnt)

		if cnt > 0 {
			var sizes []int64
			if err := client.File.Query().Where(file.UserIDEQ(userID), file.BucketIDEQ(b.ID)).
				Aggregate(ent.Sum(file.FieldSize)).Scan(l.ctx, &sizes); err != nil {
				return response.Err(errors.New("统计桶内文件大小失败: " + err.Error()))
			}
			if len(sizes) > 0 {
				stat.TotalSize = sizes[0]
			}
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
