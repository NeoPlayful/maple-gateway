// 现有表结构迁移：gateway_instances（多实例 HA：本 Gateway 进程注册表 + lease）
package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// GatewayInstance 对应 gateway_instances 表。
// 每个运行的 Gateway 进程注册一行，靠心跳续期维持 online；
// Leader（若多实例协调启用）在该行记录 lease_until。
type GatewayInstance struct {
	ent.Schema
}

func (GatewayInstance) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		// instance_id 是 Gateway 启动时生成/配置的唯一标识（addr 可能因重启漂移）。
		field.String("instance_id").NotEmpty(),
		field.String("addr").NotEmpty(),
		field.String("hostname").Default(""),
		// status: online / offline / draining / maintenance
		field.String("status").Default("online"),
		// role: standalone / leader / follower；standalone=未启用多实例协调
		field.String("role").Default("standalone"),
		field.Time("lease_until").Optional().Nillable(),
		field.String("version").Default(""),
		field.Time("started_at").Optional().Nillable(),
		field.Time("last_seen_at").Optional().Nillable(),
		field.Time("created_at").Immutable(),
		field.Time("updated_at"),
	}
}

func (GatewayInstance) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("instance_id").Unique(),
		index.Fields("status"),
		index.Fields("role"),
	}
}
