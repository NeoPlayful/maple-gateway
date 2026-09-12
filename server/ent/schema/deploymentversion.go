// 现有表结构迁移：deployment_versions
package schema

import (
	"encoding/json"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// DeploymentVersion 对应 deployment_versions 表。
type DeploymentVersion struct {
	ent.Schema
}

func (DeploymentVersion) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("deployment_id", uuid.UUID{}),
		field.String("version").NotEmpty(),
		field.String("image").Optional().Default(""),
		field.Int("weight").Default(100),
		field.String("status").Default("stable"),
		// 容器执行规格：由 Container Manager 读取并驱动 Node Agent 创建容器。
		field.Int("replicas").Default(1),
		field.Int("port").Optional().Default(0),
		field.JSON("env", map[string]string{}).Optional(),
		field.JSON("resources", json.RawMessage{}).Optional(),
		field.String("health_path").Optional().Default(""),
		field.JSON("node_selector", map[string]string{}).Optional(),
		field.Time("created_at").Immutable(),
		field.Time("updated_at"),
	}
}

func (DeploymentVersion) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("deployment", Deployment.Type).
			Ref("versions").
			Field("deployment_id").
			Unique().
			Required(),
	}
}

func (DeploymentVersion) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("deployment_id", "version").Unique(),
	}
}
