// 现有表结构迁移：certificates
// Phase 5 Direct TLS：每域名 Manual Certificate，私钥加密存储。
package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// Certificate 对应 certificates 表。
type Certificate struct {
	ent.Schema
}

func (Certificate) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("domain_id", uuid.UUID{}).Optional().Nillable(),
		// 冗余存 hostname，缓存/检索/指标免 join；与 domain.hostname 同源。
		field.String("hostname").NotEmpty(),
		// source: manual / cloudflare / acme / origin（本期只落 manual，余为枚举预留）。
		field.String("source").Default("manual"),
		// status: active / pending / error / expiring / expired（证书自身状态，与 Domain 路由状态分离）。
		field.String("status").Default("pending"),
		field.Text("certificate_pem").NotEmpty(),
		// private_key_encrypted 为 AES-GCM 密文(base64)；明文私钥永不落库/回显。
		field.Text("private_key_encrypted").NotEmpty(),
		field.String("issuer").Optional().Default(""),
		field.String("serial_number").Optional().Default(""),
		field.Time("issued_at").Optional().Nillable(),
		field.Time("expires_at").Optional().Nillable(),
		field.Time("last_renewed_at").Optional().Nillable(),
		field.String("last_error").Optional().Default(""),
		field.Time("created_at").Immutable(),
		field.Time("updated_at"),
	}
}

func (Certificate) Edges() []ent.Edge {
	return []ent.Edge{
		// M2O: 证书属于某个域名（可空）。
		edge.From("domain", Domain.Type).
			Ref("certificates").
			Field("domain_id").
			Unique(),
	}
}

func (Certificate) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("domain_id"),
		index.Fields("hostname"),
		index.Fields("expires_at"),
	}
}
