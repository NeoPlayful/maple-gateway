// 现有表结构迁移：services
package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// Service 对应 services 表。
type Service struct {
	ent.Schema
}

func (Service) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.String("name").NotEmpty(),
		field.String("protocol").Default("http"),
		field.String("status").Default("active"),
		field.Time("created_at").Immutable(),
		field.Time("updated_at"),
	}
}

func (Service) Edges() []ent.Edge {
	return []ent.Edge{
		// M2O: service 属于 tenant
		edge.From("tenant", Tenant.Type).
			Ref("services").
			Field("tenant_id").
			Unique().
			Required(),
		// O2M: service 下的实例
		edge.To("instances", Instance.Type),
		// O2M: service 下的部署
		edge.To("deployments", Deployment.Type),
		// O2M: service 下的流量策略
		edge.To("traffic_policies", TrafficPolicy.Type),
	}
}

func (Service) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "name").Unique(),
	}
}
