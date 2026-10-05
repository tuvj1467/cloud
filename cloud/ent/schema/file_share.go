package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type FileShare struct {
	ent.Schema
}

func (FileShare) Fields() []ent.Field {
	withComments := true
	return []ent.Field{
		field.String("id").
			Unique().
			NotEmpty().
			Immutable().
			Comment("分享记录主键").
			Annotations(entsql.Annotation{WithComments: &withComments}),
		field.String("file_id").
			NotEmpty().
			Comment("被分享文件").
			Annotations(entsql.Annotation{WithComments: &withComments}),
		field.String("user_id").
			NotEmpty().
			Comment("用户ID").
			Annotations(entsql.Annotation{WithComments: &withComments}),
		field.String("share_code").
			Unique().
			NotEmpty().
			Comment("提取码").
			Annotations(entsql.Annotation{WithComments: &withComments}),
		field.Enum("permission").
			Values("read", "download").
			Default("read").
			Comment("权限").
			Annotations(entsql.Annotation{WithComments: &withComments}),
		field.Time("expire_at").
			Optional().
			Nillable().
			Comment("过期时间(空=永久)").
			Annotations(entsql.Annotation{WithComments: &withComments}),
		field.Int("max_count").
			Optional().
			Nillable().
			Comment("最大访问次数(空=不限)").
			Annotations(entsql.Annotation{WithComments: &withComments}),
		field.Int("used_count").
			Default(0).
			Comment("已访问次数").
			Annotations(entsql.Annotation{WithComments: &withComments}),
	}
}

func (FileShare) Mixin() []ent.Mixin {
	return []ent.Mixin{
		TimeVersion{},
	}
}

func (FileShare) Edges() []ent.Edge {
	return nil
}

// 索引
func (FileShare) Indexes() []ent.Index {
	return []ent.Index{
		// 我的分享列表（user_id 维度 Count+List）
		index.Fields("user_id"),
		// 文件删除/桶删除时按 file_id 级联查分享
		index.Fields("file_id"),
	}
}
