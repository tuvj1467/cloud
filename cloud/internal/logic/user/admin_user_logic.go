package userlogic

import (
	"context"
	"errors"

	"onecloud/ent"
	"onecloud/internal/pkg/jwt"
	"onecloud/internal/pkg/response"
	"onecloud/internal/repository"
	"onecloud/internal/svc"
	"onecloud/proto/.proto/service"

	"github.com/zeromicro/go-zero/core/logx"
)

// ===== 管理后台公共守卫 =====

// adminDenied 管理权限不足（业务码 40300）
func adminDenied() (*service.Response, error) {
	return response.ErrWithCode(response.CodeAdminDenied, "需要管理员权限")
}

// AdminGuard 校验当前登录者为管理员；非 admin 返回拒绝响应（ok=false）
func AdminGuard(ctx context.Context) (*service.Response, bool) {
	if jwt.GetRoleFromCtx(ctx) != "admin" {
		r, _ := adminDenied()
		return r, false
	}
	return nil, true
}

// ===== 用户列表 =====

type AdminListUsersLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewAdminListUsersLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AdminListUsersLogic {
	return &AdminListUsersLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *AdminListUsersLogic) AdminListUsers(in *service.AdminListUsersRequest) (*service.Response, error) {
	if r, ok := AdminGuard(l.ctx); !ok {
		return r, nil
	}
	if err := in.Validate(); err != nil {
		return response.Err(errors.New("参数校验失败"))
	}

	list, total, err := repository.AdminListUsers(l.svcCtx.EntClient, l.ctx, int(in.GetPage()), int(in.GetPageSize()), in.GetKeyword())
	if err != nil {
		return response.ErrInternal(l.Logger, "查询用户列表失败", err)
	}

	// 聚合本页用户的文件用量（单条 GROUP BY）
	ids := make([]string, 0, len(list))
	for _, u := range list {
		ids = append(ids, u.ID)
	}
	usage, err := repository.AdminUserFileUsage(l.svcCtx.EntClient, l.ctx, ids)
	if err != nil {
		l.Errorf("聚合用户文件用量失败: %v", err)
	}

	items := make([]*service.AdminUserItem, 0, len(list))
	for _, u := range list {
		item := &service.AdminUserItem{
			UserId:    u.ID,
			Username:  u.Username,
			Nickname:  u.Nickname,
			Role:      string(u.Role),
			Disabled:  u.Disabled,
			CreatedAt: u.CreatedAt.Format("2006-01-02 15:04:05"),
		}
		if u.Phone != "" {
			item.Phone = &u.Phone
		}
		if u.Email != "" {
			item.Email = &u.Email
		}
		if use, ok := usage[u.ID]; ok {
			item.FileCount = use.Count
			item.TotalSize = use.Size
		}
		items = append(items, item)
	}
	return response.Success(&service.AdminListUsersResponse{Users: items, Total: int32(total)})
}

// ===== 角色设置 =====

type AdminSetUserRoleLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewAdminSetUserRoleLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AdminSetUserRoleLogic {
	return &AdminSetUserRoleLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *AdminSetUserRoleLogic) AdminSetUserRole(in *service.AdminSetUserRoleRequest) (*service.Response, error) {
	if r, ok := AdminGuard(l.ctx); !ok {
		return r, nil
	}
	if err := in.Validate(); err != nil {
		return response.Err(errors.New("参数校验失败"))
	}

	operatorID, _ := jwt.GetUserIdFromCtx(l.ctx)
	if operatorID == in.GetUserId() {
		return response.Err(errors.New("不能修改自己的角色"))
	}

	if err := repository.AdminSetUserRole(l.svcCtx.EntClient, l.ctx, in.GetUserId(), in.GetRole()); err != nil {
		if ent.IsNotFound(err) {
			return response.ErrWithCode(response.CodeUserNotFound, "用户不存在")
		}
		return response.ErrInternal(l.Logger, "设置角色失败", err)
	}
	if err := repository.NewAdminAuditLogRepository(l.svcCtx.EntClient).
		Create(l.ctx, operatorID, "set_role", in.GetUserId(), in.GetRole()); err != nil {
		l.Errorf("写管理审计失败: %v", err)
	}
	l.Infof("admin set role operator=%s target=%s role=%s", operatorID, in.GetUserId(), in.GetRole())
	return response.Success(&service.AdminSetUserRoleResponse{Success: true})
}

// ===== 禁用/启用 =====

type AdminSetUserDisabledLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewAdminSetUserDisabledLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AdminSetUserDisabledLogic {
	return &AdminSetUserDisabledLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *AdminSetUserDisabledLogic) AdminSetUserDisabled(in *service.AdminSetUserDisabledRequest) (*service.Response, error) {
	if r, ok := AdminGuard(l.ctx); !ok {
		return r, nil
	}
	if err := in.Validate(); err != nil {
		return response.Err(errors.New("参数校验失败"))
	}

	operatorID, _ := jwt.GetUserIdFromCtx(l.ctx)
	if operatorID == in.GetUserId() {
		return response.Err(errors.New("不能禁用自己"))
	}

	if err := repository.AdminSetUserDisabled(l.svcCtx.EntClient, l.ctx, in.GetUserId(), in.GetDisabled()); err != nil {
		if ent.IsNotFound(err) {
			return response.ErrWithCode(response.CodeUserNotFound, "用户不存在")
		}
		return response.ErrInternal(l.Logger, "设置禁用状态失败", err)
	}
	if err := repository.NewAdminAuditLogRepository(l.svcCtx.EntClient).
		Create(l.ctx, operatorID, "set_disabled", in.GetUserId(), map[bool]string{true: "disable", false: "enable"}[in.GetDisabled()]); err != nil {
		l.Errorf("写管理审计失败: %v", err)
	}
	l.Infof("admin set disabled operator=%s target=%s disabled=%v", operatorID, in.GetUserId(), in.GetDisabled())
	return response.Success(&service.AdminSetUserDisabledResponse{Success: true})
}
