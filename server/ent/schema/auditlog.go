// 现有表结构迁移：audit_logs
package schema

import (
	"encoding/json"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// AuditLog 对应 audit_logs 表（管理操作审计，无 updated_at）。
type AuditLog struct {
	ent.Schema
}

func (AuditLog) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("admin_id", uuid.UUID{}).Optional().Nillable(),
		field.String("action").NotEmpty(),
		field.String("target_type").NotEmpty(),
		field.String("target_id").Optional().Nillable(),
		field.JSON("detail", json.RawMessage{}).Optional(),
		field.String("ip").Optional().Nillable(),
		field.Time("created_at").Immutable(),
	}
}

func (AuditLog) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("action"),
		index.Fields("created_at"),
	}
}
