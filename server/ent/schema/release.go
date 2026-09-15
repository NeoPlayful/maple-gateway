// 统一发布模型：releases（合并旧 canary_releases 与 bluegreen_deployments）。
package schema

import (
	"encoding/json"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// Release 对应 releases 表（strategy 判别 canary / bluegreen）。
type Release struct {
	ent.Schema
}

func (Release) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.String("strategy").NotEmpty(), // canary / bluegreen
		field.UUID("deployment_id", uuid.UUID{}),
		field.UUID("service_id", uuid.UUID{}).Optional().Nillable(),
		field.String("name").Default(""),
		field.String("phase").Default("created"),
		// primary=基准版本（canary 的 stable / bluegreen 的 active）；
		// secondary=挑战版本（canary 的 canary / bluegreen 的另一色）。
		field.UUID("primary_version_id", uuid.UUID{}),
		field.UUID("secondary_version_id", uuid.UUID{}),
		field.Int("primary_weight").Default(100),
		field.Int("secondary_weight").Default(0),
		field.UUID("previous_primary_id", uuid.UUID{}).Optional().Nillable(),
		field.JSON("config", json.RawMessage{}).Default(json.RawMessage("{}")),
		field.Time("started_at").Optional().Nillable(),
		field.Time("finished_at").Optional().Nillable(),
		field.Time("created_at").Immutable(),
		field.Time("updated_at"),
	}
}

func (Release) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("deployment_id"),
		index.Fields("service_id"),
		index.Fields("phase"),
		// 部署级互斥：每部署至多一个非终态发布（终态 = completed/rolled_back/failed）。
		// 堵住过去 canary 与 bluegreen 可同时活跃于同一部署的漏洞。
		index.Fields("deployment_id").Unique().
			Annotations(entsql.IndexWhere("phase NOT IN ('completed','rolled_back','failed')")),
	}
}
