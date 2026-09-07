// Package canary 实现 Canary 发布状态机与流量控制。
//
// 只做流量编排（版本权重/状态迁移），容器创建销毁归 Container Manager。
// 状态机：created → running ⇄ paused → completed / rolled_back；failed 由异常触发。
package canary

import (
	"time"

	"github.com/google/uuid"
)

// Phase 是发布阶段的粗粒度状态。
type Phase string

const (
	PhaseCreated    Phase = "created"
	PhaseRunning    Phase = "running"
	PhasePaused     Phase = "paused"
	PhaseCompleted  Phase = "completed"
	PhaseRolledBack Phase = "rolled_back"
	PhaseFailed     Phase = "failed"
)

// Action 是发布事件动作（canary_events.phase 复用该语义）。
type Action string

const (
	ActionStart    Action = "start"
	ActionPause    Action = "pause"
	ActionResume   Action = "resume"
	ActionWeight   Action = "weight"
	ActionPromote  Action = "promote"
	ActionRollback Action = "rollback"
)

// Release 是一次 Canary 发布记录。
type Release struct {
	ID              uuid.UUID  `json:"id"`
	ServiceID       uuid.UUID  `json:"service_id"`
	Name            string     `json:"name"`
	StableVersionID uuid.UUID  `json:"stable_version_id"`
	CanaryVersionID uuid.UUID  `json:"canary_version_id"`
	Phase           Phase      `json:"phase"`
	CanaryWeight    int        `json:"canary_weight"`
	TargetWeight    int        `json:"target_weight"`
	StepWeight      int        `json:"step_weight"`
	StartedAt       *time.Time `json:"started_at"`
	FinishedAt      *time.Time `json:"finished_at"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// Event 是发布事件流水。
type Event struct {
	ID         uuid.UUID `json:"id"`
	ReleaseID  uuid.UUID `json:"release_id"`
	Action     Action    `json:"action"`
	FromWeight *int      `json:"from_weight"`
	ToWeight   *int      `json:"to_weight"`
	Detail     string    `json:"detail"`
	CreatedAt  time.Time `json:"created_at"`
}

// NewRelease 创建发布输入。
type NewRelease struct {
	ServiceID       uuid.UUID `json:"service_id" validate:"required"`
	Name            string    `json:"name" validate:"required,min=1,max=64"`
	StableVersionID uuid.UUID `json:"stable_version_id" validate:"required"`
	CanaryVersionID uuid.UUID `json:"canary_version_id" validate:"required"`
	InitialWeight   int       `json:"initial_weight" validate:"min=1,max=100"` // 首次 start 的 canary 权重
	TargetWeight    int       `json:"target_weight" validate:"omitempty,min=1,max=100"`
	StepWeight      int       `json:"step_weight" validate:"omitempty,min=1,max=100"`
}

// UpdateRelease 改配置（weight/step 语义由动作执行）。
type UpdateRelease struct {
	Name         *string `json:"name"`
	TargetWeight *int    `json:"target_weight"`
	StepWeight   *int    `json:"step_weight"`
}

// WeightInput 调整 canary 权重请求体。
type WeightInput struct {
	Weight int `json:"weight" validate:"required,min=0,max=100"`
}

// View 是带版本快照的发布详情（handler 组装用）。
type View struct {
	Release
	ServiceName   string `json:"service_name"`
	StableVersion string `json:"stable_version"`
	CanaryVersion string `json:"canary_version"`
}
