package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
)

type FilePart struct {
	ent.Schema
}

func (FilePart) Fields() []ent.Field {
	withComments := true
	return []ent.Field{
		field.String("id").
			Unique().
			NotEmpty().
			Immutable().
			Comment("分片记录主键").
			Annotations(entsql.Annotation{WithComments: &withComments}),
		field.String("upload_id").
			NotEmpty().
			Comment("分片上传唯一标识").
			Annotations(entsql.Annotation{WithComments: &withComments}),
		field.String("user_id").
			NotEmpty().
			Comment("用户ID").
			Annotations(entsql.Annotation{WithComments: &withComments}),
		field.String("bucket_id").
			NotEmpty().
			Comment("桶ID").
			Annotations(entsql.Annotation{WithComments: &withComments}),
		field.String("file_name").
			NotEmpty().
			Comment("文件名").
			Annotations(entsql.Annotation{WithComments: &withComments}),
		field.String("file_suffix").
			NotEmpty().
			Comment("文件后缀").
			Annotations(entsql.Annotation{WithComments: &withComments}),
		field.Int64("file_size").
			Default(0).
			Comment("文件总大小").
			Annotations(entsql.Annotation{WithComments: &withComments}),
		field.Int("total_parts").
			Default(0).
			Comment("总分片数").
			Annotations(entsql.Annotation{WithComments: &withComments}),
		field.Int("part_number").
			Comment("分片序号(从1开始)").
			Annotations(entsql.Annotation{WithComments: &withComments}),
		field.Enum("status").
			Values("pending", "uploaded", "merged").
			Default("pending").
			Comment("状态").
			Annotations(entsql.Annotation{WithComments: &withComments}),
		field.Int64("part_size").
			Default(0).
			Comment("分片大小").
			Annotations(entsql.Annotation{WithComments: &withComments}),
		field.String("part_md5").
			Optional().
			Comment("分片MD5").
			Annotations(entsql.Annotation{WithComments: &withComments}),
		field.String("part_path").
			Optional().
			Comment("分片存储路径").
			Annotations(entsql.Annotation{WithComments: &withComments}),
	}
}

func (FilePart) Mixin() []ent.Mixin {
	return []ent.Mixin{
		TimeVersion{},
	}
}

func (FilePart) Edges() []ent.Edge {
	return nil
}
