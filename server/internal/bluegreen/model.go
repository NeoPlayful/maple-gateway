// Package bluegreen 实现 Blue/Green 双版本切换。
//
// 一个部署下配 blue/green 双版本，任一时刻一方 active(weight 100) 接收流量，
// 另一方 standby(weight 0)。switch 翻转角色，rollback 切回上一 active。
// 容器创建/销毁归 Container Manager，本包只做版本状态与权重编排。
package bluegreen

import (
	"time"

	"github.com/google/uuid"
)

// BGDeployment 是一次 Blue/Green 部署配置（deployment 级唯一）。
type BGDeployment struct {
	ID               uuid.UUID `json:"id"`
	DeploymentID     uuid.UUID `json:"deployment_id"`
	BlueVersionID    uuid.UUID `json:"blue_version_id"`
	GreenVersionID   uuid.UUID `json:"green_version_id"`
	ActiveVersionID  uuid.UUID `json:"active_version_id"`
	PreviousActiveID *uuid.UUID `json:"previous_active_id"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// Event 是切换历史。
type Event struct {
	ID         uuid.UUID  `json:"id"`
	BGID       uuid.UUID  `json:"bg_id"`
	Action     string     `json:"action"` // switch / rollback
	FromActive uuid.UUID  `json:"from_active"`
	ToActive   uuid.UUID  `json:"to_active"`
	Detail     string     `json:"detail"`
	CreatedAt  time.Time  `json:"created_at"`
}

// NewBG 创建输入。
type NewBG struct {
	DeploymentID   uuid.UUID `json:"deployment_id" validate:"required"`
	BlueVersionID  uuid.UUID `json:"blue_version_id" validate:"required"`
	GreenVersionID uuid.UUID `json:"green_version_id" validate:"required"`
	InitialActive  uuid.UUID `json:"initial_active"` // 缺省取 blue
}

// View 带版本/服务名的详情快照。
type View struct {
	BGDeployment
	DeploymentName  string `json:"deployment_name"`
	BlueVersion     string `json:"blue_version"`
	GreenVersion    string `json:"green_version"`
	ActiveVersion   string `json:"active_version"`
}
