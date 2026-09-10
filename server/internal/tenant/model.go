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
//
// Description 为三态语义：非空=设置；description_clear=true=显式清空；二者皆否=
// 保持不变。description 为 JSON null 与缺省无法区分（均为 nil），故清空走独立布尔标记。
type Update struct {
	Name             *string `json:"name"`
	Description      *string `json:"description"`
	ClearDescription bool    `json:"description_clear,omitempty"`
	Status           *Status `json:"status"`
}
