// 统一发布模型：release_events（合并旧 canary_events 与 bluegreen_events）。
package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// ReleaseEvent 对应 release_events 表（发布事件流水，无 updated_at）。
type ReleaseEvent struct {
	ent.Schema
}

func (ReleaseEvent) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("release_id", uuid.UUID{}),
		field.String("action").NotEmpty(), // start/pause/resume/weight/promote/rollback/switch
		field.Int("from_weight").Optional().Nillable(),
		field.Int("to_weight").Optional().Nillable(),
		field.UUID("from_version", uuid.UUID{}).Optional().Nillable(),
		field.UUID("to_version", uuid.UUID{}).Optional().Nillable(),
		field.String("detail").Default(""),
		field.Time("created_at").Immutable(),
	}
}

func (ReleaseEvent) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("release_id"),
	}
}
