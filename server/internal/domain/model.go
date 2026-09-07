// Package domain 管理 Domain → Tenant 映射。
package domain

import (
	"time"

	"github.com/google/uuid"
)

// Status 域名状态。
type Status string

const (
	StatusActive   Status = "active"
	StatusDisabled Status = "disabled"
	StatusPending  Status = "pending"
)

// Domain 是自定义域名与 Tenant 的绑定关系。
type Domain struct {
	ID         uuid.UUID  `json:"id"`
	TenantID   uuid.UUID  `json:"tenant_id"`
	Hostname   string     `json:"hostname"`
	ServiceID  *uuid.UUID `json:"service_id,omitempty"`
	Status     Status     `json:"status"`
	VerifiedAt *time.Time `json:"verified_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// New 创建输入。
type New struct {
	TenantID  uuid.UUID  `json:"tenant_id" validate:"required"`
	Hostname  string     `json:"hostname" validate:"required"`
	ServiceID *uuid.UUID `json:"service_id"`
}

// Update 可修改字段。
type Update struct {
	Hostname  *string    `json:"hostname"`
	ServiceID *uuid.UUID `json:"service_id"`
	Status    *Status    `json:"status"`
}
