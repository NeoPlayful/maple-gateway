// settings_history 表：Settings 值历史（供按版本回滚）。
package schema

import (
	"encoding/json"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// SettingHistory 对应 settings_history 表。无 updated_at，只追加。
type SettingHistory struct {
	ent.Schema
}

func (SettingHistory) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.String("section").NotEmpty(),
		field.String("key").NotEmpty(),
		field.Int("version"),
		field.JSON("value", json.RawMessage{}),
		field.Time("created_at"),
	}
}

func (SettingHistory) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("section", "key", "version"),
	}
}

// Annotations 显式固定表名为 settings_history（与 SQL 迁移 0010 一致），
// 避免 ent 默认复数化推断成 setting_histories 导致写历史报错。
func (SettingHistory) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "settings_history"}}
}
