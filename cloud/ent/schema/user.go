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
		field.String("nickname").
			NotEmpty().
			Comment("用户名").
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
		index.Fields("nickname").
			Unique(),
	}
}
