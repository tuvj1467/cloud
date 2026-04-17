package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"onecloud/ent/ent"
	"onecloud/ent/ent/user"
	"onecloud/internal/pkg/snowId"
	"onecloud/proto/.proto/service"
)

type UserRepository struct {
	client *ent.Client
	db     *sql.DB
}

func NewUserRepository(client *ent.Client) *UserRepository {
	return &UserRepository{client: client}
}

func (r *UserRepository) CreateByAccount(ctx context.Context, snowflake *snowId.Snowflake, name string, password string) (string, error) {
	id := snowflake.MustIdS()
	return id, r.client.User.Create().
		SetID(id). //雪花ID
		SetNickname(name).
		SetPassword(password).
		SetAvatar("1").
		SetNillableEmail(nil).
		SetNillablePhone(nil).
		Exec(ctx)
}

func (r *UserRepository) CreateByPhone(ctx context.Context, snowflake *snowId.Snowflake, phone, name, password string) (string, error) {
	id := snowflake.MustIdS()
	return id, r.client.User.Create().
		SetID(id). //雪花ID
		SetNickname(name).
		SetPassword(password).
		SetAvatar("1").
		SetNillableEmail(nil).
		SetPhone(phone).
		Exec(ctx)
}

func (r *UserRepository) CreateByEmail(ctx context.Context, snowflake *snowId.Snowflake, email, name, password string) (string, error) {
	id := snowflake.MustIdS()
	return id, r.client.User.Create().
		SetID(id). //雪花ID
		SetNickname(name).
		SetPassword(password).
		SetAvatar("1").
		SetEmail(email).
		SetNillablePhone(nil).
		Exec(ctx)
}

func (r *UserRepository) GetById(ctx context.Context, id string) *ent.User {
	res, err := r.client.User.Get(ctx, id)
	if err != nil {
		return nil
	}
	return res
}

func (r *UserRepository) LoginByName(ctx context.Context, in *service.LoginByAccountReq) (*ent.User, error) {
	res, err := r.client.User.Get(ctx, in.Account)
	if err != nil {
		if ent.IsNotFound(err) {
			fmt.Println("用户不存在")
			return nil, errors.New("用户不存在")
		}

		return nil, err
	}
	if res.Password != in.Password {
		return nil, errors.New("密码或账号错误")
	}

	return res, nil
}

func (r *UserRepository) LoginByPhone(ctx context.Context, in *service.LoginByAccountReq) (*ent.User, error) {
	res, err := r.client.User.Query().
		Where(user.PhoneEQ(in.GetAccount())). //查找相同的phone
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			fmt.Println("用户不存在")
			return nil, errors.New("用户不存在")
		}
		return nil, err
	}
	if res.Password != in.Password {
		return nil, errors.New("密码或电话错误")
	}

	return res, nil
}

func (r *UserRepository) LoginByEmail(ctx context.Context, in *service.LoginByAccountReq) (*ent.User, error) {
	res, err := r.client.User.Query().
		Where(user.EmailEQ(in.GetAccount())). //查找相同的email
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			fmt.Println("用户不存在")
			return nil, errors.New("用户不存在")
		}
		return nil, err
	}
	if res.Password != in.Password { //
		return nil, errors.New("密码或电话错误")
	}

	return res, nil
}

//==========验证码登录=========//

// 手机验证码
func (r *UserRepository) LoginByPhoneSms(ctx context.Context, in *service.LoginByPhoneSmsReq) (*ent.User, error) {
	res, err := r.client.User.Query().
		Where(user.PhoneEQ(in.GetPhone())).
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			fmt.Println("用户不存在")
			return nil, errors.New("用户不存在")
		}
		return nil, err
	}

	return res, nil
}

// 邮箱验证码
func (r *UserRepository) LoginByEmailSms(ctx context.Context, in *service.LoginByEmailSmsReq) (*ent.User, error) {
	res, err := r.client.User.Query().
		Where(user.EmailEQ(in.GetEmail())).
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			fmt.Println("用户不存在")
			return nil, errors.New("用户不存在")
		}
		return nil, err
	}

	return res, nil
}

// 重置密码
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
	return r.client.User.UpdateOneID(res.ID).
		SetPassword(in.GetNewPassword()).
		Exec(ctx)
}

// 找回
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

//func (r *UserRepository) Update(ctx context.Context, u *ent.User) error {
//	return r.client.User.UpdateOne(u).
//		SetNickname(u.Nickname).
//		Save(ctx)
//}
