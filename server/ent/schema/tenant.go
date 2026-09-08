// 现有表结构迁移：tenants
package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// Tenant 对应 tenants 表。
type Tenant struct {
	ent.Schema
}

func (Tenant) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.String("name").NotEmpty(),
		field.String("slug").NotEmpty(),
		field.String("status").Default("active"),
		field.String("description").Default(""),
		field.Time("created_at").Immutable(),
		field.Time("updated_at"),
	}
}

func (Tenant) Edges() []ent.Edge {
	return []ent.Edge{
		// domains / services 通过外键引用 tenant
		edge.To("domains", Domain.Type),
		edge.To("services", Service.Type),
	}
}

func (Tenant) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("slug").Unique(),
	}
}
