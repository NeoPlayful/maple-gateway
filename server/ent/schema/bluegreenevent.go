// 现有表结构迁移：bluegreen_events
package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// BluegreenEvent 对应 bluegreen_events 表（切换历史，无 updated_at）。
type BluegreenEvent struct {
	ent.Schema
}

func (BluegreenEvent) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("bg_id", uuid.UUID{}),
		field.String("action").NotEmpty(), // switch / rollback
		field.UUID("from_active", uuid.UUID{}).Optional().Nillable(),
		field.UUID("to_active", uuid.UUID{}).Optional().Nillable(),
		field.String("detail").Optional().Default(""),
		field.Time("created_at").Immutable(),
	}
}

func (BluegreenEvent) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("bg_id"),
	}
}
