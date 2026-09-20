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

// Service 是项目下的服务（对应一套实例）。归属项目：一项目一服务。
type Service struct {
	ID        uuid.UUID  `json:"id"`
	TenantID  uuid.UUID  `json:"tenant_id"`
	ProjectID *uuid.UUID `json:"project_id,omitempty"`
	Name      string     `json:"name"`
	Protocol  string     `json:"protocol"` // http / https
	Status    Status     `json:"status"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// New 创建输入。ProjectID 为所属项目；tenant_id 从该项目继承（无需单独传）。
type New struct {
	ProjectID uuid.UUID `json:"project_id" validate:"required"`
	Name      string    `json:"name" validate:"required,min=1,max=64"`
	Protocol  string    `json:"protocol" validate:"omitempty,oneof=http https"`
}

// Update 可修改字段。ProjectID 用于调整服务归属（可修复存量未归属服务）。
type Update struct {
	ProjectID *uuid.UUID `json:"project_id"`
	Name      *string    `json:"name"`
	Protocol  *string    `json:"protocol"`
	Status    *Status    `json:"status"`
}
