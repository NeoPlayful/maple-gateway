// Package release 是统一的发布编排：把金丝雀（canary）与蓝绿（bluegreen）
// 两种发布策略收敛到同一套记录模型、同一套效果写入与同一条部署级互斥约束下。
//
// 统一的是"记录模型 + 流量效果 + 不变量"三处；**不统一生命周期语义**——
// canary 是带进度的过程（created→running→completed），
// bluegreen 是二元常驻配置（active），各自的状态迁移与权重取值范围留在 strategy 实现里。
//
// 只做流量编排（版本权重/状态），容器创建销毁归 Container Manager。
package release

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Strategy 是发布策略判别。
type Strategy string

const (
	StrategyCanary    Strategy = "canary"
	StrategyBlueGreen Strategy = "bluegreen"
)

// ValidStrategy 判断策略值合法。
func ValidStrategy(s Strategy) bool {
	return s == StrategyCanary || s == StrategyBlueGreen
}

// Phase 是发布阶段。词汇表由 strategy 解释：
//   - canary: created / running / paused / completed / rolled_back / failed
//   - bluegreen: active（二元配置，无进度与终态概念）
type Phase string

const (
	PhaseCreated    Phase = "created"
	PhaseRunning    Phase = "running"
	PhasePaused     Phase = "paused"
	PhaseCompleted  Phase = "completed"
	PhaseRolledBack Phase = "rolled_back"
	PhaseFailed     Phase = "failed"
	// PhaseActive 是 bluegreen 的唯一 phase：表示该二元配置已生效。
	PhaseActive Phase = "active"
)

// Terminal 判断 phase 是否为终态（终态发布不占用部署级活跃名额）。
func (p Phase) Terminal() bool {
	switch p {
	case PhaseCompleted, PhaseRolledBack, PhaseFailed:
		return true
	}
	return false
}

// Action 是发布事件动作（release_events.action）。
type Action string

const (
	ActionStart    Action = "start"
	ActionPause    Action = "pause"
	ActionResume   Action = "resume"
	ActionWeight   Action = "weight"
	ActionPromote  Action = "promote"
	ActionRollback Action = "rollback"
	ActionSwitch   Action = "switch"
)

// Release 是一次发布记录。
//
// primary=基准版本（canary 的 stable / bluegreen 的 active）；
// secondary=挑战版本（canary 的 canary / bluegreen 的另一色）。
type Release struct {
	ID                 uuid.UUID       `json:"id"`
	Strategy           Strategy        `json:"strategy"`
	DeploymentID       uuid.UUID       `json:"deployment_id"`
	ServiceID          *uuid.UUID      `json:"service_id,omitempty"`
	Name               string          `json:"name"`
	Phase              Phase           `json:"phase"`
	PrimaryVersionID   uuid.UUID       `json:"primary_version_id"`
	SecondaryVersionID uuid.UUID       `json:"secondary_version_id"`
	PrimaryWeight      int             `json:"primary_weight"`
	SecondaryWeight    int             `json:"secondary_weight"`
	PreviousPrimaryID  *uuid.UUID      `json:"previous_primary_id,omitempty"`
	Config             json.RawMessage `json:"config"`
	StartedAt          *time.Time      `json:"started_at,omitempty"`
	FinishedAt         *time.Time      `json:"finished_at,omitempty"`
	CreatedAt          time.Time       `json:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
}

// Event 是发布事件流水。
type Event struct {
	ID          uuid.UUID  `json:"id"`
	ReleaseID   uuid.UUID  `json:"release_id"`
	Action      Action     `json:"action"`
	FromWeight  *int       `json:"from_weight,omitempty"`
	ToWeight    *int       `json:"to_weight,omitempty"`
	FromVersion *uuid.UUID `json:"from_version,omitempty"`
	ToVersion   *uuid.UUID `json:"to_version,omitempty"`
	Detail      string     `json:"detail"`
	CreatedAt   time.Time  `json:"created_at"`
}

// CanaryConfig 是 canary 策略的专属配置（存 releases.config）。
type CanaryConfig struct {
	TargetWeight int `json:"target_weight"`
	StepWeight   int `json:"step_weight"`
}

// BlueGreenConfig 是 bluegreen 策略的专属配置（存 releases.config）。
type BlueGreenConfig struct {
	BlueVersionID  uuid.UUID `json:"blue_version_id"`
	GreenVersionID uuid.UUID `json:"green_version_id"`
}

// NewRelease 创建发布输入（通用体，按 strategy 解释字段）。
type NewRelease struct {
	Strategy     Strategy   `json:"strategy" validate:"required"`
	ServiceID    *uuid.UUID `json:"service_id"`
	Name         string     `json:"name" validate:"required,min=1,max=64"`
	DeploymentID uuid.UUID  `json:"deployment_id"`
	// canary: PrimaryVersionID=stable, SecondaryVersionID=canary
	// bluegreen: PrimaryVersionID=缺省激活色(可选), SecondaryVersionID 不用
	PrimaryVersionID   uuid.UUID `json:"primary_version_id"`
	SecondaryVersionID uuid.UUID `json:"secondary_version_id"`
	// canary 专属
	InitialWeight int `json:"initial_weight"`
	TargetWeight  int `json:"target_weight"`
	StepWeight    int `json:"step_weight"`
	// bluegreen 专属：显式指定 blue/green 两色
	BlueVersionID  uuid.UUID `json:"blue_version_id"`
	GreenVersionID uuid.UUID `json:"green_version_id"`
}

// UpdateRelease 改配置（不改 phase 与实时权重）。
type UpdateRelease struct {
	Name         *string `json:"name"`
	TargetWeight *int    `json:"target_weight"`
	StepWeight   *int    `json:"step_weight"`
}

// WeightInput 调整 canary 权重请求体。
type WeightInput struct {
	Weight int `json:"weight" validate:"required,min=0,max=100"`
}

// SwitchInput 切换 bluegreen active 请求体。
type SwitchInput struct {
	TargetVersionID uuid.UUID `json:"target_version_id" validate:"required"`
}

// View 是带版本/服务名快照的发布详情（handler 组装用）。
type View struct {
	Release
	ServiceName      string `json:"service_name,omitempty"`
	DeploymentName   string `json:"deployment_name,omitempty"`
	PrimaryVersion   string `json:"primary_version,omitempty"`
	SecondaryVersion string `json:"secondary_version,omitempty"`
}
