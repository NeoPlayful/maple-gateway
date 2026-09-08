// 现有表结构迁移：deployments
package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// Deployment 对应 deployments 表。
type Deployment struct {
	ent.Schema
}

func (Deployment) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("service_id", uuid.UUID{}),
		field.String("name").NotEmpty(),
		field.String("status").Default("active"),
		field.String("strategy").Default("rolling"),
		field.Time("created_at").Immutable(),
		field.Time("updated_at"),
	}
}

func (Deployment) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("service", Service.Type).
			Ref("deployments").
			Field("service_id").
			Unique().
			Required(),
		edge.To("versions", DeploymentVersion.Type),
	}
}

func (Deployment) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("service_id", "name").Unique(),
	}
}
