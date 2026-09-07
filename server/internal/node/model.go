// Package node 管理计算节点（Node Agent / Container Manager 宿主）。
package node

import (
	"time"

	"github.com/google/uuid"
)

// Status 是节点状态。
type Status string

const (
	StatusOnline      Status = "online"
	StatusOffline     Status = "offline"
	StatusMaintenance Status = "maintenance"
	StatusDisabled    Status = "disabled"
)

// Node 是一个运行实例的计算节点。
type Node struct {
	ID         uuid.UUID   `json:"id"`
	Name       string      `json:"name"`
	Host       string      `json:"host"`
	Region     string      `json:"region,omitempty"`
	Labels     map[string]string `json:"labels,omitempty"`
	Status     Status      `json:"status"`
	Weight     int         `json:"weight"`
	LastSeenAt *time.Time  `json:"last_seen_at,omitempty"`
	CreatedAt  time.Time   `json:"created_at"`
	UpdatedAt  time.Time   `json:"updated_at"`
}

// New 携带创建输入。
type New struct {
	Name   string            `json:"name" validate:"required,min=1,max=100"`
	Host   string            `json:"host" validate:"required,min=1,max=255"`
	Region string            `json:"region" validate:"max=64"`
	Labels map[string]string `json:"labels"`
	Weight int               `json:"weight" validate:"min=0,max=1000"`
}

// Update 携带可修改字段。
type Update struct {
	Host    *string           `json:"host"`
	Region  *string           `json:"region"`
	Labels  map[string]string `json:"labels"`
	Weight  *int              `json:"weight"`
	Status  *Status           `json:"status"`
}

// Heartbeat 更新节点最后心跳时间。
func (n *Node) Heartbeat() {
	now := time.Now()
	n.LastSeenAt = &now
}
