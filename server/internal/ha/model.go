// Package ha 管理 Gateway 自身多实例高可用：
// 本进程在 gateway_instances 注册一行、心跳续期；多实例协调启用时
// 竞逐 Leader（Redis 锁优先，断连降级 DB lease）。
package ha

import (
	"time"

	"github.com/google/uuid"
)

// Status 是 Gateway 实例状态。
type Status string

const (
	StatusOnline      Status = "online"
	StatusOffline     Status = "offline"
	StatusDraining    Status = "draining"
	StatusMaintenance Status = "maintenance"
)

// Role 是 Gateway 实例在多实例协调中的角色。
type Role string

const (
	RoleStandalone Role = "standalone" // 未启用多实例协调（单实例默认）
	RoleLeader     Role = "leader"
	RoleFollower   Role = "follower"
)

// Instance 描述一个运行中的 Gateway 进程实例。
type Instance struct {
	ID         uuid.UUID  `json:"id"`
	InstanceID string     `json:"instance_id"` // 启动时生成/配置，重启稳定标识
	Addr       string     `json:"addr"`        // 管理面监听地址（协调/诊断用）
	Hostname   string     `json:"hostname"`
	Status     Status     `json:"status"`
	Role       Role       `json:"role"`
	LeaseUntil *time.Time `json:"lease_until,omitempty"`
	Version    string     `json:"version,omitempty"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	LastSeenAt *time.Time `json:"last_seen_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// Register 携带注册输入。
type Register struct {
	InstanceID string `json:"instance_id" validate:"required,min=1,max=100"`
	Addr       string `json:"addr" validate:"required,min=1,max=255"`
	Hostname   string `json:"hostname" validate:"max=255"`
	Version    string `json:"version" validate:"max=64"`
}

// Config 是 HA 协调参数。
type Config struct {
	InstanceID string        // 本进程实例 ID
	Addr       string        // 本进程管理面地址
	Hostname   string        // 本进程主机名
	Version    string        // 应用版本（pkg.Version）
	Enabled    bool          // 是否启用多实例协调（Leader 竞逐）
	Heartbeat  time.Duration // 心跳周期
	LeaseTTL   time.Duration // lease 时长
}
