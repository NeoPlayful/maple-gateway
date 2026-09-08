// 现有表结构迁移：admins（0001 起 uuid 主键；email 唯一）
package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// Admin 对应 admins 表（Management API 登录账号）。
type Admin struct {
	ent.Schema
}

func (Admin) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.String("email").NotEmpty(),
		field.String("password_hash").NotEmpty(),
		field.String("name").Optional().Nillable(),
		field.String("status").Default("active"),
		field.Time("created_at").Immutable(),
		field.Time("updated_at"),
	}
}

func (Admin) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("email").Unique(),
	}
}
