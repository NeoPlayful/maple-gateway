// 现有表结构迁移：canary_events
package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// CanaryEvent 对应 canary_events 表（发布事件流水，无 updated_at）。
type CanaryEvent struct {
	ent.Schema
}

func (CanaryEvent) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("release_id", uuid.UUID{}),
		field.String("phase").NotEmpty(), // 动作：start / pause / resume / weight / promote / rollback
		field.Int("from_weight").Optional().Nillable(),
		field.Int("to_weight").Optional().Nillable(),
		field.String("detail").Optional().Default(""),
		field.Time("created_at").Immutable(),
	}
}

func (CanaryEvent) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("release_id"),
	}
}
