package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ShareAccessLog 分享访问审计日志：AccessShare（查看）与 DownloadByShare（下载）逐次记录
type ShareAccessLog struct {
	ent.Schema
}

func (ShareAccessLog) Fields() []ent.Field {
	withComments := true
	return []ent.Field{
		field.String("id").
			Unique().
			NotEmpty().
			Comment("雪花ID").
			Annotations(entsql.Annotation{WithComments: &withComments}),
		field.String("share_id").
			NotEmpty().
			Comment("分享记录ID").
			Annotations(entsql.Annotation{WithComments: &withComments}),
		field.String("share_code").
			NotEmpty().
			Comment("提取码").
			Annotations(entsql.Annotation{WithComments: &withComments}),
		field.String("action").
			NotEmpty().
			Comment("动作：access 查看 / download 下载").
			Annotations(entsql.Annotation{WithComments: &withComments}),
		field.String("ip").
			Optional().
			Default("").
			Comment("客户端 IP（X-Forwarded-For 优先，回退 gRPC 对端）").
			Annotations(entsql.Annotation{WithComments: &withComments}),
		field.String("user_agent").
			Optional().
			Default("").
			Comment("客户端 UA").
			Annotations(entsql.Annotation{WithComments: &withComments}),
	}
}

// TimeVersion 混入提供 created_at（审计日志按其倒序展示）
func (ShareAccessLog) Mixin() []ent.Mixin {
	return []ent.Mixin{
		TimeVersion{},
	}
}

func (ShareAccessLog) Edges() []ent.Edge {
	return nil
}

func (ShareAccessLog) Indexes() []ent.Index {
	return []ent.Index{
		// 日志查询：按提取码 + 时间倒序
		index.Fields("share_code", "created_at"),
	}
}
