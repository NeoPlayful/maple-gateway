// Managed Certificate：certificate_operations 表 —— 签发/续期进度。
package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// CertificateOperation 对应 certificate_operations 表：记录一次 ACME 签发/续期的阶段进度，
// 供管理面轮询展示。与 certificates 表分离——进行中的操作没有可用证书材料，
// 绝不进证书表，避免扰动数据面缓存装载（ReloadAll 只应看到成品）。
type CertificateOperation struct {
	ent.Schema
}

func (CertificateOperation) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		// hostname 冗余存，便于按域名查最新进度。
		field.String("hostname").NotEmpty(),
		field.UUID("domain_id", uuid.UUID{}).Optional().Nillable(),
		// action: issue / renew
		field.String("action").Default("issue"),
		// status: queued / account_ready / order_created / challenge_presented /
		// challenge_validated / finalizing / active / failed
		field.String("status").Default("queued"),
		// message 人类可读的阶段说明（如"等待 CA 验证域名"）。
		field.String("message").Optional().Default(""),
		// error 失败原因（已分类，可回显给管理员）。
		field.String("error").Optional().Default(""),
		field.Time("started_at"),
		field.Time("updated_at"),
		field.Time("finished_at").Optional().Nillable(),
	}
}

func (CertificateOperation) Indexes() []ent.Index {
	return []ent.Index{
		// 同一 hostname 同时仅允许一个进行中的操作（去重，防重复下单触发 CA 限流）。
		index.Fields("hostname").Unique().
			Annotations(entsql.IndexWhere("status NOT IN ('active','failed')")),
		index.Fields("domain_id"),
		index.Fields("updated_at"),
	}
}
