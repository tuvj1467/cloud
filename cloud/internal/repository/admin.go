package repository

import (
	"context"

	"onecloud/ent"
	"onecloud/ent/file"
	"onecloud/ent/user"
)

// AdminListUsers 管理后台用户列表（分页 + 可选关键词，匹配 username/nickname/phone）
func AdminListUsers(client *ent.Client, ctx context.Context, page, pageSize int, keyword string) ([]*ent.User, int, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 10
	}
	q := client.User.Query()
	if keyword != "" {
		q = q.Where(
			user.Or(
				user.UsernameContains(keyword),
				user.NicknameContains(keyword),
				user.PhoneContains(keyword),
			),
		)
	}
	total, err := q.Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	list, err := q.Order(user.ByCreatedAt()).
		Offset((page - 1) * pageSize).Limit(pageSize).All(ctx)
	if err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

// AdminSetUserRole 设置用户角色（仅 user/admin）
func AdminSetUserRole(client *ent.Client, ctx context.Context, userID, role string) error {
	return client.User.UpdateOneID(userID).
		SetRole(user.Role(role)).
		Exec(ctx)
}

// AdminSetUserDisabled 设置禁用状态
func AdminSetUserDisabled(client *ent.Client, ctx context.Context, userID string, disabled bool) error {
	upd := client.User.UpdateOneID(userID).SetDisabled(disabled)
	if disabled {
		upd.AddTokenVersion(1) // 禁用即踢出既有会话（token ver 失配）
	}
	return upd.Exec(ctx)
}

// AdminUserFileUsage 聚合一组用户的文件数与总大小（user_id → 行）
func AdminUserFileUsage(client *ent.Client, ctx context.Context, userIDs []string) (map[string]struct {
	Count int64
	Size  int64
}, error) {
	res := map[string]struct {
		Count int64
		Size  int64
	}{}
	if len(userIDs) == 0 {
		return res, nil
	}
	var rows []struct {
		UserID string `json:"user_id"`
		Count  int64  `json:"count"`
		Sum    int64  `json:"sum"`
	}
	err := client.File.Query().
		Where(file.UserIDIn(userIDs...)).
		GroupBy(file.FieldUserID).
		Aggregate(ent.Count(), ent.Sum(file.FieldSize)).
		Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		res[row.UserID] = struct {
			Count int64
			Size  int64
		}{row.Count, row.Sum}
	}
	return res, nil
}

// AdminGlobalCounts 全局统计：用户数/文件数/总容量
func AdminGlobalCounts(client *ent.Client, ctx context.Context) (users, files int, totalSize int64, err error) {
	users, err = client.User.Query().Count(ctx)
	if err != nil {
		return
	}
	files, err = client.File.Query().Count(ctx)
	if err != nil {
		return
	}
	var sumRows []struct {
		Sum int64 `json:"sum"`
	}
	if err = client.File.Query().Aggregate(ent.Sum(file.FieldSize)).Scan(ctx, &sumRows); err != nil {
		return
	}
	if len(sumRows) > 0 {
		totalSize = sumRows[0].Sum
	}
	return
}
