package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/mixin"
)

// TimeVersion 定义通用的时间和版本号字段 Mixin
type TimeVersion struct {
	mixin.Schema // 嵌入 ent 的 mixin.Schema，作为 Mixin 的基类
}

// Fields 定义要复用的字段：创建时间、更新时间、版本号
func (TimeVersion) Fields() []ent.Field {
	return []ent.Field{
		// 创建时间：默认当前时间，不可更新
		field.Time("created_at").
			Default(time.Now).
			Comment("创建时间"),
		// 更新时间：默认当前时间，更新时自动刷新
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now).
			Comment("更新时间"),
		// 版本号：默认 1，用于乐观锁
		field.Int("version").
			Default(1).
			Comment("版本号（乐观锁）"),
	}
}
