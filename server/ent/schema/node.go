// 现有表结构迁移：nodes
package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// Node 对应 nodes 表。
type Node struct {
	ent.Schema
}

func (Node) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.String("name").NotEmpty(),
		field.String("host").NotEmpty(),
		field.String("region").Optional().Default(""),
		// data_dir 是容器绑定挂载的宿主根目录：其下按 <租户>/<模板>/<项目> 分目录隔离。
		// 为空表示未配置，不做绑定挂载。
		field.String("data_dir").Default(""),
		field.JSON("labels", map[string]string{}).Optional(),
		field.String("status").Default("online"),
		field.Int("weight").Default(1),
		field.Time("last_seen_at").Optional().Nillable(),
		field.Time("created_at").Immutable(),
		field.Time("updated_at"),
	}
}

func (Node) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("name").Unique(),
		index.Fields("status"),
	}
}
