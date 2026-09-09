// 现有表结构迁移：domains
package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// Domain 对应 domains 表。
type Domain struct {
	ent.Schema
}

func (Domain) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.String("hostname").NotEmpty(),
		field.UUID("service_id", uuid.UUID{}).Optional().Nillable(),
		field.String("status").Default("active"),
		field.Time("verified_at").Optional().Nillable(),
		// Phase 5 Direct TLS：TLS 接入模式（cloudflare/managed/manual/disabled；本期可写 manual|disabled）。
		field.String("tls_mode").Default("disabled"),
		// Phase 5 Direct TLS：证书自身状态（active/pending/error/expiring/expired），与 status(路由)分离。
		field.String("certificate_status").Optional().Nillable(),
		field.Time("created_at").Immutable(),
		field.Time("updated_at"),
	}
}

func (Domain) Edges() []ent.Edge {
	return []ent.Edge{
		// M2O: domain 属于 tenant
		edge.From("tenant", Tenant.Type).
			Ref("domains").
			Field("tenant_id").
			Unique().
			Required(),
		// O2M: domain 下的证书（Direct TLS，一般 0..1 生效）。
		edge.To("certificates", Certificate.Type),
	}
}

func (Domain) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("hostname").Unique(),
	}
}
