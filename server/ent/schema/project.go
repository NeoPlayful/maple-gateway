// 新表：projects —— 租户下的一个部署项目。
package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// Project 对应 projects 表。
//
// 一个租户 + 一个模板可以有多个项目；项目名即项目标识（必须英文），
// 用于区分同一租户同一模板下的多份独立部署，并作为数据目录的第三段。
type Project struct {
	ent.Schema
}

func (Project) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		// template_id 指向项目使用的模板（templates.id）。
		field.UUID("template_id", uuid.UUID{}),
		// name 是项目标识（英文），同一租户内唯一，也是数据目录第三段。
		field.String("name").NotEmpty(),
		field.String("description").Optional().Default(""),
		field.String("status").Default("active"),
		// node_id 是部署落点（Gateway 节点 UUID）；空表示尚未指定。
		field.String("node_id").Optional().Default(""),
		// application_id 是渲染模板后生成的 CM Application ID，作为实例化的幂等键。
		field.String("application_id").Optional().Default(""),
		field.Time("created_at").Immutable(),
		field.Time("updated_at"),
	}
}

func (Project) Indexes() []ent.Index {
	return []ent.Index{
		// 同一租户下项目标识唯一（路径唯一性键的前两段之外的最后一段）。
		index.Fields("tenant_id", "name").Unique(),
		index.Fields("tenant_id"),
	}
}
