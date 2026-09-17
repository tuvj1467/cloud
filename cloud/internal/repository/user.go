package repository

import (
	"context"
	"errors"
	"fmt"
	"onecloud/ent"
	"onecloud/ent/user"
	"onecloud/internal/pkg/loginlock"
	"onecloud/internal/pkg/passwd"
	"onecloud/internal/pkg/snowId"
	"onecloud/proto/.proto/service"
)

type UserRepository struct {
	client *ent.Client
}

func NewUserRepository(client *ent.Client) *UserRepository {
	return &UserRepository{client: client}
}

// CreateByAccount 用户名注册（密码使用 bcrypt 加密）
func (r *UserRepository) CreateByAccount(ctx context.Context, snowflake *snowId.Snowflake, name string, password string) (string, error) {
	id := snowflake.MustIdS()
	hashed, err := passwd.Hash(password)
	if err != nil {
		return id, fmt.Errorf("密码加密失败: %w", err)
	}
	return id, r.client.User.Create().
		SetID(id). // 雪花ID
		SetUsername(name).
		SetNickname(name).
		SetPassword(hashed).
		SetAvatar("1").
		SetNillableEmail(nil).
		SetNillablePhone(nil).
		Exec(ctx)
}

// CreateByPhone 手机号注册（密码使用 bcrypt 加密）
func (r *UserRepository) CreateByPhone(ctx context.Context, snowflake *snowId.Snowflake, phone, name, password string) (string, error) {
	id := snowflake.MustIdS()
	// oneof 注册时 name 恒为空，昵称兜底为注册联系方式（NotEmpty 校验必需）
	if name == "" {
		name = phone
	}
	hashed, err := passwd.Hash(password)
	if err != nil {
		return id, fmt.Errorf("密码加密失败: %w", err)
	}
	return id, r.client.User.Create().
		SetID(id). // 雪花ID
		// 不设置 username（Optional 字段）→ DB 写 NULL（username 唯一，仅用户名注册占用）
		SetNickname(name).
		SetPassword(hashed).
		SetAvatar("1").
		SetNillableEmail(nil).
		SetPhone(phone).
		Exec(ctx)
}

// CreateByEmail 邮箱注册（密码使用 bcrypt 加密）
func (r *UserRepository) CreateByEmail(ctx context.Context, snowflake *snowId.Snowflake, email, name, password string) (string, error) {
	id := snowflake.MustIdS()
	// oneof 注册时 name 恒为空，昵称兜底为注册联系方式（NotEmpty 校验必需）
	if name == "" {
		name = email
	}
	hashed, err := passwd.Hash(password)
	if err != nil {
		return id, fmt.Errorf("密码加密失败: %w", err)
	}
	return id, r.client.User.Create().
		SetID(id). // 雪花ID
		// 不设置 username（Optional 字段）→ DB 写 NULL（username 唯一，仅用户名注册占用）
		SetNickname(name).
		SetPassword(hashed).
		SetAvatar("1").
		SetEmail(email).
		SetNillablePhone(nil).
		Exec(ctx)
}

// GetById 根据 id 查询用户
func (r *UserRepository) GetById(ctx context.Context, id string) (*ent.User, error) {
	return r.client.User.Get(ctx, id)
}

// ExistsByUsername 判断用户名是否已被占用（注册预检，避免唯一索引冲突的裸 DB 错误）
func (r *UserRepository) ExistsByUsername(ctx context.Context, name string) (bool, error) {
	return r.client.User.Query().
		Where(user.UsernameEQ(name)).
		Exist(ctx)
}

// LoginByName 用户名登录
// 支持存量明文密码渐进式迁移：若 DB 中是明文，校验通过后自动升级为 bcrypt
func (r *UserRepository) LoginByName(ctx context.Context, in *service.LoginByAccountReq) (*ent.User, error) {
	res, err := r.client.User.Query().
		Where(user.UsernameEQ(in.Account)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, errors.New("用户名或密码错误")
		}
		return nil, err
	}
	if locked, remain := loginlock.Locked(ctx, res.Username); locked {
		return nil, fmt.Errorf("账号已被锁定，请 %d 分钟后重试", int(remain.Minutes())+1)
	}
	if err := r.verifyAndUpgrade(ctx, res, in.Password); err != nil {
		loginlock.RecordFail(ctx, res.Username)
		return nil, err
	}
	loginlock.Clear(ctx, res.Username)
	return res, nil
}

// LoginByPhone 手机号登录
// 支持存量明文密码渐进式迁移
func (r *UserRepository) LoginByPhone(ctx context.Context, in *service.LoginByAccountReq) (*ent.User, error) {
	res, err := r.client.User.Query().
		Where(user.PhoneEQ(in.GetAccount())). // 查找相同的phone
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return nil, errors.New("用户名或密码错误")
		}
		return nil, err
	}
	if locked, remain := loginlock.Locked(ctx, res.Username); locked {
		return nil, fmt.Errorf("账号已被锁定，请 %d 分钟后重试", int(remain.Minutes())+1)
	}
	if err := r.verifyAndUpgrade(ctx, res, in.Password); err != nil {
		loginlock.RecordFail(ctx, res.Username)
		return nil, err
	}
	loginlock.Clear(ctx, res.Username)
	return res, nil
}

// LoginByEmail 邮箱登录
// 支持存量明文密码渐进式迁移
func (r *UserRepository) LoginByEmail(ctx context.Context, in *service.LoginByAccountReq) (*ent.User, error) {
	res, err := r.client.User.Query().
		Where(user.EmailEQ(in.GetAccount())). // 查找相同的email
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return nil, errors.New("用户名或密码错误")
		}
		return nil, err
	}
	if locked, remain := loginlock.Locked(ctx, res.Username); locked {
		return nil, fmt.Errorf("账号已被锁定，请 %d 分钟后重试", int(remain.Minutes())+1)
	}
	if err := r.verifyAndUpgrade(ctx, res, in.Password); err != nil {
		loginlock.RecordFail(ctx, res.Username)
		return nil, err
	}
	loginlock.Clear(ctx, res.Username)
	return res, nil
}

// verifyAndUpgrade 校验密码并自动升级存量明文密码为 bcrypt
// 1. 若已是 bcrypt 哈希：直接 Compare
// 2. 若是明文：明文匹配后异步升级为 bcrypt
// 3. 都不匹配：返回「用户名或密码错误」（不区分账号是否存在，防爆破）
func (r *UserRepository) verifyAndUpgrade(ctx context.Context, res *ent.User, plain string) error {
	if passwd.IsHashed(res.Password) {
		// 已加密：直接校验
		if err := passwd.Compare(res.Password, plain); err != nil {
			return errors.New("用户名或密码错误")
		}
		return nil
	}

	// 明文兜底（存量数据）
	if res.Password != plain {
		return errors.New("用户名或密码错误")
	}

	// 命中明文，升级为 bcrypt（失败仅告警，不影响登录）
	if hashed, err := passwd.Hash(plain); err == nil {
		if err := r.client.User.UpdateOneID(res.ID).
			SetPassword(hashed).
			Exec(ctx); err != nil {
			fmt.Printf("密码自动升级失败 user_id=%s: %v\n", res.ID, err)
		}
	}
	return nil
}

//==========验证码登录=========//

// LoginByPhoneSms 手机验证码登录
func (r *UserRepository) LoginByPhoneSms(ctx context.Context, in *service.LoginByPhoneSmsReq) (*ent.User, error) {
	res, err := r.client.User.Query().
		Where(user.PhoneEQ(in.GetPhone())).
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return nil, errors.New("用户不存在")
		}
		return nil, err
	}

	return res, nil
}

// LoginByEmailSms 邮箱验证码登录
func (r *UserRepository) LoginByEmailSms(ctx context.Context, in *service.LoginByEmailSmsReq) (*ent.User, error) {
	res, err := r.client.User.Query().
		Where(user.EmailEQ(in.GetEmail())).
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return nil, errors.New("用户不存在")
		}
		return nil, err
	}

	return res, nil
}

// ResetPassword 重置密码（使用 bcrypt 加密新密码）
func (r *UserRepository) ResetPassword(ctx context.Context, in *service.ResetPasswordReq) error {
	var res *ent.User
	var err error
	switch in.GetContact().(type) {
	case *service.ResetPasswordReq_Phone:
		res, err = r.client.User.Query().Where(user.PhoneEQ(in.GetPhone())).Only(ctx)
		if err != nil {
			return err
		}
	case *service.ResetPasswordReq_Email:
		res, err = r.client.User.Query().Where(user.EmailEQ(in.GetEmail())).Only(ctx)
		if err != nil {
			return err
		}
	}

	hashed, err := passwd.Hash(in.GetNewPassword())
	if err != nil {
		return fmt.Errorf("密码加密失败: %w", err)
	}
	if err := r.client.User.UpdateOneID(res.ID).
		SetPassword(hashed).
		AddTokenVersion(1). // 重置密码后踢出该用户所有既有 token
		Exec(ctx); err != nil {
		return err
	}
	return nil
}

// FindBack 找回账号
func (r *UserRepository) FindBack(ctx context.Context, in *service.FindAccountReq) (*ent.User, error) {
	var res *ent.User
	var err error
	switch in.GetContact().(type) {
	case *service.FindAccountReq_Phone:
		res, err = r.client.User.Query().Where(user.PhoneEQ(in.GetPhone())).Only(ctx)
		if err != nil {
			return nil, err
		}
	case *service.FindAccountReq_Email:
		res, err = r.client.User.Query().Where(user.EmailEQ(in.GetEmail())).Only(ctx)
		if err != nil {
			return nil, err
		}
	}
	return r.client.User.Get(ctx, res.ID)
}
