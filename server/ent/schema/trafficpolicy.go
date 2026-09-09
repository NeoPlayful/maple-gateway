// 现有表结构迁移：traffic_policies
package schema

import (
	"encoding/json"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// TrafficPolicy 对应 traffic_policies 表。
type TrafficPolicy struct {
	ent.Schema
}

func (TrafficPolicy) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("service_id", uuid.UUID{}),
		field.String("name").NotEmpty(),
		field.Int("priority").Default(100),
		field.JSON("match", json.RawMessage{}).Default(json.RawMessage("{}")),
		field.UUID("target_version_id", uuid.UUID{}).Optional().Nillable(),
		field.Int("weight").Default(100),
		field.JSON("sticky", json.RawMessage{}).Optional(),
		// balance 指定命中该策略后的实例选择算法：round_robin / consistent_hash / least_conn。
		field.String("balance").Default("round_robin"),
		field.String("status").Default("enabled"),
		field.Time("created_at").Immutable(),
		field.Time("updated_at"),
	}
}

func (TrafficPolicy) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("service", Service.Type).
			Ref("traffic_policies").
			Field("service_id").
			Unique().
			Required(),
	}
}

func (TrafficPolicy) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("service_id", "name").Unique(),
	}
}
