package filelogic

import (
	"context"
	"errors"
	"time"

	"onecloud/ent"
	"onecloud/internal/pkg/cache"
	"onecloud/internal/pkg/jwt"
	"onecloud/internal/pkg/response"
	"onecloud/internal/pkg/util"
	"onecloud/internal/repository"
	"onecloud/internal/svc"
	"onecloud/proto/.proto/service"

	"github.com/zeromicro/go-zero/core/logx"
)

type CreateShareLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCreateShareLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateShareLogic {
	return &CreateShareLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// CreateShare 创建文件分享
// 步骤：校验文件归属 → 生成 share_code（碰撞重试）→ 计算 expire_at
//
//	→ 写 DB → 写 Redis 缓存 → 返回 share_code 与 share_url
func (l *CreateShareLogic) CreateShare(in *service.CreateShareRequest) (*service.Response, error) {
	// 1. 校验入参
	if err := in.Validate(); err != nil {
		return response.Err(errors.New("参数校验失败: " + err.Error()))
	}

	// 2. 获取当前用户
	userID, err := jwt.GetUserIdFromCtx(l.ctx)
	if err != nil {
		return response.Err(err)
	}

	fileRepo := repository.NewFileRepository(l.svcCtx.EntClient)
	shareRepo := repository.NewFileShareRepository(l.svcCtx.EntClient)
	shareCache := cache.NewShareCache(l.svcCtx.Redis)

	// 3. 校验文件归属
	f, err := fileRepo.GetInfo(l.ctx, in.GetFileId())
	if err != nil {
		if ent.IsNotFound(err) {
			return response.Err(errors.New("文件不存在"))
		}
		return response.Err(errors.New("查询文件失败: " + err.Error()))
	}
	if f.UserID != userID {
		return response.Err(errors.New("无权分享该文件"))
	}
	// 回收站中的文件不可分享（软删后至彻底删除前也不允许新增分享入口）
	if f.DeletedAt != nil {
		return response.Err(errors.New("文件已在回收站中，无法分享"))
	}

	// 4. 生成 share_code（碰撞重试 3 次）
	var shareCode string
	for i := 0; i < 3; i++ {
		code, err := util.GenerateShareCode(8)
		if err != nil {
			return response.Err(errors.New("生成分享码失败: " + err.Error()))
		}
		// 检查是否碰撞
		exist, _ := shareRepo.GetByCode(l.ctx, code)
		if exist == nil {
			shareCode = code
			break
		}
	}
	if shareCode == "" {
		return response.Err(errors.New("生成分享码失败（多次碰撞）"))
	}

	// 5. 生成分享记录主键
	shareID := l.svcCtx.Snowflake.MustIdS()

	// 6. 计算 expire_at（nil 表示永久）
	var expireUnix *int64
	if in.GetExpireSeconds() > 0 {
		v := time.Now().Unix() + in.GetExpireSeconds()
		expireUnix = &v
	}

	// 7. 转换 max_count（proto 是 *int32，DB 层用 *int）
	var maxCount *int
	if in.MaxCount != nil {
		v := int(*in.MaxCount)
		maxCount = &v
	}

	// 8. 写入 DB
	share, err := shareRepo.CreateWithParams(
		l.ctx, shareID, in.GetFileId(), userID, shareCode,
		in.GetPermission(), expireUnix, maxCount,
	)
	if err != nil {
		return response.Err(errors.New("创建分享记录失败: " + err.Error()))
	}

	// 9. 写入 Redis 缓存
	cacheData := &cache.ShareData{
		ID:         share.ID,
		FileID:     share.FileID,
		UserID:     share.UserID,
		ShareCode:  share.ShareCode,
		Permission: string(share.Permission),
		ExpireAt:   share.ExpireAt,
		MaxCount:   share.MaxCount,
	}
	if err := shareCache.Set(l.ctx, cacheData, 0); err != nil {
		l.Errorf("写入分享缓存失败（不影响主流程）: %v", err)
	}

	// 9. 拼接 share_url（gateway 地址从配置获取，这里用相对路径）
	shareURL := "/file/share/access?code=" + shareCode

	l.Infof("create share success user=%s file=%s code=%s permission=%s",
		userID, in.GetFileId(), shareCode, in.GetPermission())

	return response.Success(&service.CreateShareResponse{
		ShareCode: shareCode,
		ShareUrl:  shareURL,
	})
}
