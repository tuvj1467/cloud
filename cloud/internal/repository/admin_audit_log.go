package repository

import (
	"context"

	"onecloud/ent"
	"onecloud/ent/adminauditlog"
	"onecloud/internal/pkg/snowId"
)

// AdminAuditLogRepository 管理操作审计日志数据访问层
type AdminAuditLogRepository struct {
	client *ent.Client
}

func NewAdminAuditLogRepository(client *ent.Client) *AdminAuditLogRepository {
	return &AdminAuditLogRepository{client: client}
}

// Create 写入一条管理操作审计（action: set_role / set_disabled / purge_file）
func (r *AdminAuditLogRepository) Create(ctx context.Context, adminID, action, targetID, detail string) error {
	return r.client.AdminAuditLog.Create().
		SetID(snowId.Instance.MustIdS()).
		SetAdminID(adminID).
		SetAction(action).
		SetTargetID(targetID).
		SetDetail(detail).
		Exec(ctx)
}

// ListRecent 全局最近管理操作审计（时间倒序，admin 面板展示用）
func (r *AdminAuditLogRepository) ListRecent(ctx context.Context, limit int) ([]*ent.AdminAuditLog, error) {
	if limit <= 0 {
		limit = 50
	}
	return r.client.AdminAuditLog.Query().
		Order(adminauditlog.ByCreatedAt()).
		Limit(limit).
		All(ctx)
}
