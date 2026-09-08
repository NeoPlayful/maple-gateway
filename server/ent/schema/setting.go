// 现有表结构迁移：settings（0009 起 id 单列主键；section+key 唯一）
package schema

import (
	"encoding/json"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// Setting 对应 settings 表。
type Setting struct {
	ent.Schema
}

func (Setting) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.String("section").NotEmpty(),
		field.String("key").NotEmpty(),
		field.JSON("value", json.RawMessage{}).Default(json.RawMessage("null")),
		field.Int("version").Default(1),
		field.Time("updated_at"),
	}
}

func (Setting) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("section", "key").Unique(),
		index.Fields("section"),
	}
}
