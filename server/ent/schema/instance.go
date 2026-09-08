// 现有表结构迁移：instances
package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// Instance 对应 instances 表。
type Instance struct {
	ent.Schema
}

func (Instance) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("service_id", uuid.UUID{}),
		// node_id / deployment_id / version_id 现有外键引用；S3 建模对应表后再决定是否升为 edge
		field.UUID("node_id", uuid.UUID{}).Optional().Nillable(),
		field.UUID("deployment_id", uuid.UUID{}).Optional().Nillable(),
		field.UUID("version_id", uuid.UUID{}).Optional().Nillable(),
		field.String("version").Default(""),
		field.String("address").NotEmpty(),
		field.Int("port"),
		field.String("protocol").Default("http"),
		field.Int("weight").Default(1),
		field.String("status").Default("enabled"),
		field.String("health").Default("unknown"),
		field.Time("last_seen_at").Optional().Nillable(),
		field.Time("created_at").Immutable(),
		field.Time("updated_at"),
	}
}

func (Instance) Edges() []ent.Edge {
	return []ent.Edge{
		// M2O: instance 属于 service
		edge.From("service", Service.Type).
			Ref("instances").
			Field("service_id").
			Unique().
			Required(),
	}
}

func (Instance) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("health"),
	}
}
