package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// AdminAuditLog 管理操作审计日志：改角色/禁用/强制删除等管理动作逐次记录
type AdminAuditLog struct {
	ent.Schema
}

func (AdminAuditLog) Fields() []ent.Field {
	withComments := true
	return []ent.Field{
		field.String("id").
			Unique().
			NotEmpty().
			Comment("雪花ID").
			Annotations(entsql.Annotation{WithComments: &withComments}),
		field.String("admin_id").
			NotEmpty().
			Comment("操作管理员ID").
			Annotations(entsql.Annotation{WithComments: &withComments}),
		field.String("action").
			NotEmpty().
			Comment("动作：set_role / set_disabled / purge_file").
			Annotations(entsql.Annotation{WithComments: &withComments}),
		field.String("target_id").
			Optional().
			Default("").
			Comment("目标对象ID（用户ID或文件ID）").
			Annotations(entsql.Annotation{WithComments: &withComments}),
		field.String("detail").
			Optional().
			Default("").
			Comment("动作详情（如新角色值）").
			Annotations(entsql.Annotation{WithComments: &withComments}),
	}
}

// TimeVersion 混入提供 created_at（审计按其倒序展示）
func (AdminAuditLog) Mixin() []ent.Mixin {
	return []ent.Mixin{
		TimeVersion{},
	}
}

func (AdminAuditLog) Edges() []ent.Edge {
	return nil
}

func (AdminAuditLog) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("created_at"),
	}
}
