// Package ratelimit 实现多维度限流（global/tenant/domain/service/ip）。
//
// 管理面：rate_limits 表 CRUD（模型见 RateLimit）。数据平面：内存滑动窗口
// 限流器（Limiter），请求命中路由后、转发前按各 scope 取交集判定，任一超限返回 429。
// 多 Gateway 共享计数（Redis）Phase 3 提供，本阶段以单机内存为准。
package ratelimit

import (
	"time"

	"github.com/google/uuid"
)

// Scope 是限流维度。
type Scope string

const (
	ScopeGlobal  Scope = "global"
	ScopeTenant  Scope = "tenant"
	ScopeDomain  Scope = "domain"
	ScopeService Scope = "service"
	ScopeIP      Scope = "ip"
)

// Status 是限流规则状态。
type Status string

const (
	StatusEnabled  Status = "enabled"
	StatusDisabled Status = "disabled"
)

// RateLimit 是一条限流规则。
type RateLimit struct {
	ID            uuid.UUID `json:"id"`
	Scope         Scope     `json:"scope"`
	TenantID      *uuid.UUID `json:"tenant_id"`
	DomainID      *uuid.UUID `json:"domain_id"`
	ServiceID     *uuid.UUID `json:"service_id"`
	Name          string    `json:"name"`
	Limit         int       `json:"limit"`
	WindowSeconds int       `json:"window_seconds"`
	Burst         int       `json:"burst"`
	ResponseCode  int       `json:"response_code"`
	Status        Status    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// NewRateLimit 创建输入。
type NewRateLimit struct {
	Scope         Scope      `json:"scope" validate:"required,oneof=global tenant domain service ip"`
	TenantID      *uuid.UUID `json:"tenant_id"`
	DomainID      *uuid.UUID `json:"domain_id"`
	ServiceID     *uuid.UUID `json:"service_id"`
	Name          string     `json:"name" validate:"required,min=1,max=64"`
	Limit         int        `json:"limit" validate:"required,min=1,max=1000000"`
	WindowSeconds int        `json:"window_seconds" validate:"required,min=1,max=86400"`
	Burst         int        `json:"burst" validate:"omitempty,min=0,max=1000000"`
	ResponseCode  int        `json:"response_code" validate:"omitempty,min=100,max=599"`
}

// UpdateRateLimit 可修改字段。
type UpdateRateLimit struct {
	Name          *string  `json:"name"`
	Limit         *int     `json:"limit"`
	WindowSeconds *int     `json:"window_seconds"`
	Burst         *int     `json:"burst"`
	ResponseCode  *int     `json:"response_code"`
	Status        *Status  `json:"status"`
}
