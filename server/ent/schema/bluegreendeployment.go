// 现有表结构迁移：bluegreen_deployments
package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// BluegreenDeployment 对应 bluegreen_deployments 表。
type BluegreenDeployment struct {
	ent.Schema
}

func (BluegreenDeployment) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("deployment_id", uuid.UUID{}),
		field.UUID("blue_version_id", uuid.UUID{}),
		field.UUID("green_version_id", uuid.UUID{}),
		field.UUID("active_version_id", uuid.UUID{}),
		field.UUID("previous_active_id", uuid.UUID{}).Optional().Nillable(),
		field.Time("created_at").Immutable(),
		field.Time("updated_at"),
	}
}

func (BluegreenDeployment) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("deployment_id").Unique(),
	}
}
