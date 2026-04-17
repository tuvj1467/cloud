package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
)

type Bucket struct {
	ent.Schema
}

// 字段定义
func (Bucket) Fields() []ent.Field {
	withComments := true
	return []ent.Field{
		field.String("id").
			Unique().
			Comment("桶主键").
			Annotations(entsql.Annotation{WithComments: &withComments}),
		field.String("name").
			Comment("桶名称（用户内唯一）").
			Annotations(entsql.Annotation{WithComments: &withComments}),
		field.String("user_id").
			NotEmpty().
			Comment("所属用户").
			Annotations(entsql.Annotation{WithComments: &withComments}),
		field.String("description").
			Optional().
			Comment("桶描述").
			Annotations(entsql.Annotation{WithComments: &withComments}),
		field.Int64("size_limit").
			Default(1024 * 1024 * 1024). // 默认 1GB 容量限制
			Comment("桶容量限制（字节）").
			Annotations(entsql.Annotation{WithComments: &withComments}),
		field.Int64("used_size").
			Default(0).
			Comment("已使用容量（字节）").
			Annotations(entsql.Annotation{WithComments: &withComments}),
		field.Enum("visibility").
			Values("private", "internal", "public").
			Default("private").
			Comment("桶访问权限").
			Annotations(entsql.Annotation{WithComments: &withComments}),
	}
}

// 嵌入通用 Mixin
func (Bucket) Mixin() []ent.Mixin {
	return []ent.Mixin{
		TimeVersion{},
	}
}

// 索引
//func (Bucket) Indexes() []ent.Index {
//	return []ent.Index{}
//}
//
//// 关联关系（桶属于一个用户，一个用户有多个桶）
//func (Bucket) Edges() []ent.Edge {
//	return []ent.Edge{}
//}
