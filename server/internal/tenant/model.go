// Package tenant 管理租户实体。
package tenant

import (
	"time"

	"github.com/google/uuid"
)

// Status 是租户状态。
type Status string

const (
	StatusActive    Status = "active"
	StatusSuspended Status = "suspended"
	StatusDisabled  Status = "disabled"
)

// Tenant 是 Maple Gateway 第一层资源。
type Tenant struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Status      Status    `json:"status"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// New 携带创建输入。
type New struct {
	Name        string `json:"name" validate:"required,min=1,max=100"`
	Slug        string `json:"slug" validate:"required,min=2,max=64"`
	Description string `json:"description" validate:"max=500"`
}

// Update 携带可修改字段。
type Update struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Status      *Status `json:"status"`
}
