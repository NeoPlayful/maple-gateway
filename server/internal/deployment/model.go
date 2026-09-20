// Package deployment 管理 Service 下的 Deployment 与 DeploymentVersion。
package deployment

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Status 是部署状态。
type Status string

const (
	StatusActive  Status = "active"
	StatusPaused  Status = "paused"
	StatusStopped Status = "stopped"
)

// Strategy 是部署策略（记录型，执行归 Container Manager）。
type Strategy string

const (
	StrategyRolling   Strategy = "rolling"
	StrategyRecreate  Strategy = "recreate"
	StrategyBlueGreen Strategy = "blue_green"
)

// Deployment 是一个 Service 下的发布单元（如 prod）。
type Deployment struct {
	ID        uuid.UUID `json:"id"`
	ServiceID uuid.UUID `json:"service_id"`
	Name      string    `json:"name"`
	Status    Status    `json:"status"`
	Strategy  Strategy  `json:"strategy"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// NewDeployment 创建部署输入。
type NewDeployment struct {
	ServiceID uuid.UUID `json:"service_id" validate:"required"`
	Name      string    `json:"name" validate:"required,min=1,max=64"`
	Strategy  Strategy  `json:"strategy" validate:"omitempty,oneof=rolling recreate blue_green"`
}

// UpdateDeployment 可修改字段。
type UpdateDeployment struct {
	Name     *string   `json:"name"`
	Status   *Status   `json:"status"`
	Strategy *Strategy `json:"strategy"`
}

// VersionStatus 是版本状态。
type VersionStatus string

const (
	VersionStable   VersionStatus = "stable"
	VersionCanary   VersionStatus = "canary"
	VersionActive   VersionStatus = "active"
	VersionStandby  VersionStatus = "standby"
	VersionDraining VersionStatus = "draining"
	VersionInactive VersionStatus = "inactive"
)

// Mount 是一次绑定挂载：把节点数据根下的子目录映射进容器。
// Path 相对节点 data_dir（形如 <租户>/<模板>/<项目>/<子目录>），宿主绝对路径由节点拼出。
type Mount struct {
	Path     string `json:"path"`
	Target   string `json:"target"`
	ReadOnly bool   `json:"read_only,omitempty"`
}

// Version 是分流的最小单元。
type Version struct {
	ID           uuid.UUID         `json:"id"`
	DeploymentID uuid.UUID         `json:"deployment_id"`
	// ProjectID 是版本所属项目（可空）。非空时该版本创建的容器带 maple.project_id 标签，
	// 使单容器与 Compose 应用共用同一项目隔离单元。
	ProjectID    *uuid.UUID        `json:"project_id,omitempty"`
	Version      string            `json:"version"`
	Image        string            `json:"image,omitempty"`
	Weight       int               `json:"weight"`
	Status       VersionStatus     `json:"status"`
	Replicas     int               `json:"replicas"`
	Port         int               `json:"port,omitempty"`
	Env          map[string]string `json:"env,omitempty"`
	Resources    json.RawMessage   `json:"resources,omitempty"`
	HealthPath   string            `json:"health_path,omitempty"`
	NodeSelector map[string]string `json:"node_selector,omitempty"`
	Mounts       []Mount           `json:"mounts,omitempty"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
}

// NewVersion 创建版本输入。
type NewVersion struct {
	Version      string            `json:"version" validate:"required,min=1,max=64"`
	// ProjectID 是版本所属项目（可空）；留空表示不属于任何项目。
	ProjectID    *uuid.UUID        `json:"project_id"`
	Image        string            `json:"image" validate:"max=255"`
	Weight       int               `json:"weight" validate:"min=0,max=1000"`
	Status       VersionStatus     `json:"status" validate:"omitempty,oneof=stable standby canary inactive"`
	Replicas     int               `json:"replicas" validate:"min=0,max=1000"`
	Port         int               `json:"port" validate:"min=0,max=65535"`
	Env          map[string]string `json:"env"`
	Resources    json.RawMessage   `json:"resources"`
	HealthPath   string            `json:"health_path" validate:"max=255"`
	NodeSelector map[string]string `json:"node_selector"`
	Mounts       []Mount           `json:"mounts"`
}

// UpdateVersion 可修改字段。
type UpdateVersion struct {
	// ProjectID 为三态：非 nil 指向 uuid.Nil 表示解除项目归属；指向有效 UUID 表示设置。
	ProjectID    *uuid.UUID        `json:"project_id"`
	Image        *string           `json:"image"`
	Weight       *int              `json:"weight"`
	Status       *VersionStatus    `json:"status"`
	Replicas     *int              `json:"replicas"`
	Port         *int              `json:"port"`
	Env          map[string]string `json:"env"`
	Resources    json.RawMessage   `json:"resources"`
	HealthPath   *string           `json:"health_path"`
	NodeSelector map[string]string `json:"node_selector"`
	Mounts       []Mount           `json:"mounts"`
}

// VersionWithOwner 是全局版本列表行：版本规格 + 所属服务/部署归属。
// 版本自身只存 deployment_id，跨服务平铺展示时需补出服务名。
type VersionWithOwner struct {
	Version
	ServiceID      uuid.UUID `json:"service_id"`
	ServiceName    string    `json:"service_name"`
	DeploymentName string    `json:"deployment_name"`
}
