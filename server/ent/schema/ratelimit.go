// 现有表结构迁移：rate_limits
package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// RateLimit 对应 rate_limits 表。
type RateLimit struct {
	ent.Schema
}

func (RateLimit) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.String("scope").NotEmpty(),
		field.UUID("tenant_id", uuid.UUID{}).Optional().Nillable(),
		field.UUID("domain_id", uuid.UUID{}).Optional().Nillable(),
		field.UUID("service_id", uuid.UUID{}).Optional().Nillable(),
		field.String("name").NotEmpty(),
		field.Int("limit"),
		field.Int("window_seconds").Default(60),
		field.Int("burst").Optional().Default(0),
		field.Int("response_code").Default(429),
		field.String("status").Default("enabled"),
		field.Time("created_at").Immutable(),
		field.Time("updated_at"),
	}
}

func (RateLimit) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("scope"),
		index.Fields("tenant_id"),
		index.Fields("domain_id"),
		index.Fields("service_id"),
	}
}
