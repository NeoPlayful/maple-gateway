// Package service 管理 Tenant 下的应用服务。
package service

import (
	"time"

	"github.com/google/uuid"
)

// Status 服务状态。
type Status string

const (
	StatusActive   Status = "active"
	StatusDisabled Status = "disabled"
)

// Service 是 Tenant 下的服务（对应一套实例）。
type Service struct {
	ID        uuid.UUID `json:"id"`
	TenantID  uuid.UUID `json:"tenant_id"`
	Name      string    `json:"name"`
	Protocol  string    `json:"protocol"` // http / https
	Status    Status    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// New 创建输入。
type New struct {
	TenantID uuid.UUID `json:"tenant_id" validate:"required"`
	Name     string    `json:"name" validate:"required,min=1,max=64"`
	Protocol string    `json:"protocol" validate:"omitempty,oneof=http https"`
}

// Update 可修改字段。
type Update struct {
	Name     *string `json:"name"`
	Protocol *string `json:"protocol"`
	Status   *Status `json:"status"`
}
