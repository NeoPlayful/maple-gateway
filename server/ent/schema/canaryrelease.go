// 现有表结构迁移：canary_releases
package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// CanaryRelease 对应 canary_releases 表。
type CanaryRelease struct {
	ent.Schema
}

func (CanaryRelease) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("service_id", uuid.UUID{}),
		field.String("name").NotEmpty(),
		field.UUID("stable_version_id", uuid.UUID{}),
		field.UUID("canary_version_id", uuid.UUID{}),
		field.String("phase").Default("created"),
		field.Int("canary_weight").Default(0),
		field.Int("target_weight").Default(100),
		field.Int("step_weight").Default(10),
		field.Time("started_at").Optional().Nillable(),
		field.Time("finished_at").Optional().Nillable(),
		field.Time("created_at").Immutable(),
		field.Time("updated_at"),
	}
}

func (CanaryRelease) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("service_id", "name").Unique(),
		index.Fields("phase"),
	}
}
