package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// StorageSnapshot 用量每日快照：每用户每天一行，记录截至当日的累计文件数与总容量
// date 为 "2006-01-02" 形态的字符串（避免时区/时分秒歧义），(date, user_id) 唯一
type StorageSnapshot struct {
	ent.Schema
}

func (StorageSnapshot) Fields() []ent.Field {
	withComments := true
	return []ent.Field{
		field.String("id").
			Unique().
			NotEmpty().
			Comment("雪花ID").
			Annotations(entsql.Annotation{WithComments: &withComments}),
		field.String("date").
			MaxLen(10).
			NotEmpty().
			Comment("快照日期（2006-01-02）").
			Annotations(entsql.Annotation{WithComments: &withComments}),
		field.String("user_id").
			NotEmpty().
			Comment("用户ID").
			Annotations(entsql.Annotation{WithComments: &withComments}),
		field.Int64("file_count").
			Default(0).
			Comment("截至当日该用户文件总数").
			Annotations(entsql.Annotation{WithComments: &withComments}),
		field.Int64("total_size").
			Default(0).
			Comment("截至当日该用户文件总大小（字节）").
			Annotations(entsql.Annotation{WithComments: &withComments}),
	}
}

func (StorageSnapshot) Mixin() []ent.Mixin {
	return nil // 快照为只写聚合数据，无需 created_at/version
}

func (StorageSnapshot) Edges() []ent.Edge {
	return nil
}

func (StorageSnapshot) Indexes() []ent.Index {
	return []ent.Index{
		// 趋势查询：某用户的日期范围；唯一约束保证幂等写入
		index.Fields("date", "user_id").Unique(),
	}
}
