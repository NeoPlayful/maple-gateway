// 新表：templates —— 应用模板，容器创建的规格来源。
package schema

import (
	"encoding/json"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// Template 对应 templates 表。
//
// 模板是一份带占位符的 Compose 规格 + 参数定义；用户选模板、填参数即可创建一个项目，
// 渲染出的规格转成 Application 下发。slug 是模板标识（英文），作为数据目录第二段。
type Template struct {
	ent.Schema
}

func (Template) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.String("name").NotEmpty(),
		// slug 是模板标识（英文），全局唯一，也是数据目录第二段。
		field.String("slug").NotEmpty().Unique(),
		field.String("description").Optional().Default(""),
		// spec 是 Compose 规格模板（YAML 原文，含 {{参数键}} 占位符）。
		field.String("spec").Default(""),
		// params 是参数定义数组：[{key,label,type,required,default,options,hint}]。
		field.JSON("params", json.RawMessage{}).Optional(),
		field.String("status").Default("active"),
		field.Time("created_at").Immutable(),
		field.Time("updated_at"),
	}
}

func (Template) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("status"),
	}
}
