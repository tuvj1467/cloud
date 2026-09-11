package repository

import (
	"context"

	"onecloud/ent"
	"onecloud/ent/shareaccesslog"
	"onecloud/internal/pkg/snowId"
)

// ShareAccessLogRepository 分享访问审计日志数据访问层
type ShareAccessLogRepository struct {
	client *ent.Client
}

func NewShareAccessLogRepository(client *ent.Client) *ShareAccessLogRepository {
	return &ShareAccessLogRepository{client: client}
}

// Create 写入一条访问/下载审计记录
func (r *ShareAccessLogRepository) Create(ctx context.Context, shareID, shareCode, action, ip, userAgent string) error {
	return r.client.ShareAccessLog.Create().
		SetID(snowId.Instance.MustIdS()).
		SetShareID(shareID).
		SetShareCode(shareCode).
		SetAction(action).
		SetIP(ip).
		SetUserAgent(userAgent).
		Exec(ctx)
}

// ListByShareCode 分页查询某提取码的审计日志（时间倒序），返回列表与总数
func (r *ShareAccessLogRepository) ListByShareCode(ctx context.Context, shareCode string, page, pageSize int) ([]*ent.ShareAccessLog, int, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 10
	}
	base := r.client.ShareAccessLog.Query().Where(shareaccesslog.ShareCodeEQ(shareCode))
	total, err := base.Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	list, err := base.Order(shareaccesslog.ByCreatedAt()).
		Offset((page - 1) * pageSize).Limit(pageSize).All(ctx)
	if err != nil {
		return nil, 0, err
	}
	return list, total, nil
}
