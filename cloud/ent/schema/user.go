package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// User 保存了 User 实体的模式定义。
type User struct {
	ent.Schema
}

func (User) Fields() []ent.Field {
	withComments := true
	return []ent.Field{
		field.String("id").
			Unique().
			NotEmpty().
			Immutable().
			Comment("雪花ID（业务层禁止修改）").
			Annotations(entsql.Annotation{WithComments: &withComments}),
		field.String("username").
			Optional().
			Comment("登录账号（不可修改）").
			Annotations(entsql.Annotation{WithComments: &withComments}),
		field.String("nickname").
			NotEmpty().
			Optional().
			Comment("显示名称（可修改）").
			Annotations(entsql.Annotation{WithComments: &withComments}),
		field.String("avatar").
			NotEmpty().
			Comment("头像 提供默认头像").
			Annotations(entsql.Annotation{WithComments: &withComments}),
		field.String("email").
			Unique().
			Optional().
			Comment("邮箱地址").
			Annotations(entsql.Annotation{WithComments: &withComments}),
		field.String("password").
			NotEmpty().
			Comment("密码").
			Annotations(entsql.Annotation{WithComments: &withComments}),
		field.String("phone").
			Unique().
			Optional().
			Comment("电话号码").
			Annotations(entsql.Annotation{WithComments: &withComments}),
		field.Enum("role").
			Values("user", "admin").
			Default("user").
			Comment("角色：user 普通用户 / admin 管理员").
			Annotations(entsql.Annotation{WithComments: &withComments}),
		field.Bool("disabled").
			Default(false).
			Comment("是否被禁用（禁用后拒绝登录与既有 token）").
			Annotations(entsql.Annotation{WithComments: &withComments}),
	}
}

func (User) Mixin() []ent.Mixin {
	return []ent.Mixin{
		TimeVersion{},
	}
}

func (User) Edges() []ent.Edge {
	return nil
}

func (User) Indexes() []ent.Index {
	return []ent.Index{
		// 昵称仅做查询索引，允许重名（原唯一索引导致重名注册裸 DB 错误）
		index.Fields("nickname"),
		// 用户名全局唯一（登录账号；手机/邮箱注册用户该列为 NULL，可多行 NULL 不冲突）
		index.Fields("username").Unique(),
	}
}
