// 现有表结构迁移：users（原 admins，0018 改名；uuid 主键；email 唯一）
package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// User 对应 users 表（Management API 登录账号 / 平台用户）。
type User struct {
	ent.Schema
}

func (User) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.String("email").NotEmpty(),
		field.String("password_hash").NotEmpty(),
		field.String("name").Optional().Nillable(),
		field.String("role").Default("super_admin"), // RBAC：super_admin / operator / viewer
		field.String("status").Default("active"),
		field.Time("created_at").Immutable(),
		field.Time("updated_at"),
	}
}

func (User) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("email").Unique(),
	}
}
