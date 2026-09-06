// ent/schema/file.go
package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type File struct {
	ent.Schema
}

// 字段定义
func (File) Fields() []ent.Field {
	withComments := true
	return []ent.Field{
		field.String("id").
			Unique().
			Comment("文件主键）").
			Annotations(entsql.Annotation{WithComments: &withComments}),
		field.String("name").
			Comment("文件名").
			Annotations(entsql.Annotation{WithComments: &withComments}),
		field.String("ext").
			Optional().
			Comment("文件扩展名（如 .jpg）").
			Annotations(entsql.Annotation{WithComments: &withComments}),
		field.String("path").
			Comment("文件存储路径（本地/对象存储）").
			Annotations(entsql.Annotation{WithComments: &withComments}),
		field.Int64("size").
			Comment("文件大小（字节）").
			Annotations(entsql.Annotation{WithComments: &withComments}),
		field.String("md5").
			Comment("文件 MD5（防篡改）").
			Annotations(entsql.Annotation{WithComments: &withComments}),
		field.String("content_type").
			Comment("文件类型（如 image/jpeg）").
			Annotations(entsql.Annotation{WithComments: &withComments}),
		field.String("bucket_id").
			Comment("文件所属桶ID").
			Annotations(entsql.Annotation{WithComments: &withComments}),
		field.String("user_id").
			NotEmpty().
			Comment("文件所属用户ID").
			Annotations(entsql.Annotation{WithComments: &withComments}),
	}
}

// 嵌入通用 Mixin
func (File) Mixin() []ent.Mixin {
	return []ent.Mixin{
		TimeVersion{},
	}
}

// 索引
func (File) Indexes() []ent.Index {
	return []ent.Index{
		// 秒传兜底查询：user_id + md5（原无索引全表扫描）
		index.Fields("md5"),
		// 桶维度列表与桶清空删除
		index.Fields("bucket_id"),
	}
}

func (File) Edges() []ent.Edge {
	return []ent.Edge{}
}
