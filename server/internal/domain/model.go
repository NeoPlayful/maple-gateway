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
	// Phase 5 Direct TLS：TLS 接入模式（cloudflare/managed/manual/disabled）。
	TLSMode string `json:"tls_mode,omitempty"`
	// Phase 5 Direct TLS：证书状态（active/pending/error/expiring/expired），与路由状态分离。
	CertificateStatus *string   `json:"certificate_status,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// New 创建输入。
type New struct {
	TenantID  uuid.UUID  `json:"tenant_id" validate:"required"`
	Hostname  string     `json:"hostname" validate:"required"`
	ServiceID *uuid.UUID `json:"service_id"`
	// TLSMode 缺省 disabled（Phase 1 语义未变）。
	TLSMode string `json:"tls_mode"`
}

// Update 可修改字段。
//
// ServiceID 是三态语义：service_id 非空=设置绑定；service_id_clear=true=显式
// 清空绑定；二者皆否=保持原绑定不变。默认为"不变"而非"清空"，避免只改其它字段
// 的部分更新（如证书联动 TLS 状态、enable/disable）误清 service_id 掉出路由。
// service_id 为 JSON null 与缺省无法区分（均为 nil），故清空必须走独立布尔标记。
type Update struct {
	Hostname       *string    `json:"hostname"`
	ServiceID      *uuid.UUID `json:"service_id"`
	ClearServiceID bool       `json:"service_id_clear,omitempty"`
	Status         *Status    `json:"status"`
	// Phase 5：TLS 相关字段由证书服务联动更新。
	TLSMode           *string `json:"tls_mode"`
	CertificateStatus *string `json:"certificate_status"`
}
