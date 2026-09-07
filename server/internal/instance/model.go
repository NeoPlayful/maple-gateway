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
	ID         uuid.UUID  `json:"id"`
	ServiceID  uuid.UUID  `json:"service_id"`
	NodeID     *uuid.UUID `json:"node_id,omitempty"`
	Version    string     `json:"version,omitempty"`
	Address    string     `json:"address"`
	Port       int        `json:"port"`
	Protocol   string     `json:"protocol"` // http / https
	Weight     int        `json:"weight"`
	Status     Status     `json:"status"`
	Health     Health     `json:"health"`
	LastSeenAt *time.Time `json:"last_seen_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// New 创建输入。
type New struct {
	ServiceID uuid.UUID  `json:"service_id" validate:"required"`
	NodeID    *uuid.UUID `json:"node_id"`
	Version   string     `json:"version"`
	Address   string     `json:"address" validate:"required"`
	Port      int        `json:"port" validate:"required,min=1,max=65535"`
	Protocol  string     `json:"protocol" validate:"omitempty,oneof=http https"`
	Weight    int        `json:"weight" validate:"min=0,max=1000"`
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

// Endpoint 返回 host:port。
func (i *Instance) Endpoint() string {
	return addrJoin(i.Address, i.Port)
}

// IsRoutable 判断实例当前是否可接收流量（healthy + enabled + 非 draining）。
func (i *Instance) IsRoutable() bool {
	return i.Status == StatusEnabled && i.Health == HealthHealthy
}
