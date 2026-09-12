// Package instance 管理实际 Container 实例。
package instance

import (
	"time"

	"github.com/google/uuid"
)

// Status 是实例流量状态。
type Status string

const (
	StatusEnabled  Status = "enabled"
	StatusDisabled Status = "disabled"
	StatusDraining Status = "draining"
	StatusStale    Status = "stale" // 上报超时：CM 长时间未看见该实例
)

// Health 是实例健康状态。
type Health string

const (
	HealthUnknown    Health = "unknown"
	HealthHealthy    Health = "healthy"
	HealthUnhealthy  Health = "unhealthy"
	HealthRecovering Health = "recovering"
)

// Instance 是一个实际运行的 Container 实例。
type Instance struct {
	ID           uuid.UUID  `json:"id"`
	ServiceID    uuid.UUID  `json:"service_id"`
	DeploymentID *uuid.UUID `json:"deployment_id,omitempty"`
	VersionID    *uuid.UUID `json:"version_id,omitempty"`
	NodeID       *uuid.UUID `json:"node_id,omitempty"`
	Version      string     `json:"version,omitempty"`
	Address      string     `json:"address"`
	Port         int        `json:"port"`
	Protocol     string     `json:"protocol"` // http / https
	Weight       int        `json:"weight"`
	Status       Status     `json:"status"`
	Health       Health     `json:"health"`
	LastSeenAt   *time.Time `json:"last_seen_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// New 创建输入。deployment_id/version_id 为空时按 Phase 1 语义直挂 Service。
type New struct {
	// ID 显式指定实例 ID；为空则数据库生成。CM 上报时用容器 maple.instance_id 标签，
	// 与 Gateway 实例 ID 一一对应。
	ID           *uuid.UUID `json:"id"`
	ServiceID    uuid.UUID  `json:"service_id" validate:"required"`
	DeploymentID *uuid.UUID `json:"deployment_id"`
	VersionID    *uuid.UUID `json:"version_id"`
	NodeID       *uuid.UUID `json:"node_id"`
	Version      string     `json:"version"`
	Address      string     `json:"address" validate:"required"`
	Port         int        `json:"port" validate:"required,min=1,max=65535"`
	Protocol     string     `json:"protocol" validate:"omitempty,oneof=http https"`
	Weight       int        `json:"weight" validate:"min=0,max=1000"`
}

// Update 可修改字段。
type Update struct {
	Address  *string `json:"address"`
	Port     *int    `json:"port"`
	Protocol *string `json:"protocol"`
	Weight   *int    `json:"weight"`
	Status   *Status `json:"status"`
	Health   *Health `json:"health"`
}

// Mount 用于调整实例在 Deployment/Version/Node 上的挂载归属。
// DeploymentID/VersionID 为 nil 表示不修改；指向 uuid.Nil 表示解挂（回退直挂 Service）。
type Mount struct {
	DeploymentID *uuid.UUID `json:"deployment_id"`
	VersionID    *uuid.UUID `json:"version_id"`
	NodeID       *uuid.UUID `json:"node_id"`
}

// Endpoint 返回 host:port。
func (i *Instance) Endpoint() string {
	return addrJoin(i.Address, i.Port)
}

// IsRoutable 判断实例当前是否可接收流量（healthy + enabled + 非 draining）。
func (i *Instance) IsRoutable() bool {
	return i.Status == StatusEnabled && i.Health == HealthHealthy
}
