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
		// tenant_id 是派生归属：从所属项目继承，路由表的租户门控仍依赖它。
		field.UUID("tenant_id", uuid.UUID{}),
		// project_id 是服务所属项目（projects.id，可空）。非空即声明「一项目一服务」，
		// 唯一索引落在该列上；存量租户级服务不填，维持可路由。
		field.UUID("project_id", uuid.UUID{}).Optional().Nillable(),
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
		// 一项目一服务：project_id 非空时唯一（NULL 不去重，兼容存量未归属服务）。
		index.Fields("project_id").Unique(),
	}
}
