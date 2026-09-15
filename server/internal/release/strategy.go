package release

import (
	"context"
	"encoding/json"

	"github.com/NeoPlayful/maple-gateway/server/ent"
	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/google/uuid"
)

// allocation 是一条发布对某版本的流量分配意图：把该版本设为 (weight, status)。
// 两个 strategy 各自"算"出 allocation，共用同一个"写"路径（service.applyAllocation）。
type allocation struct {
	VersionID uuid.UUID
	Weight    int
	Status    string // stable / canary / active / standby / draining
}

// strategy 是发布策略的差异面：如何建记录、允许哪些动作、状态如何迁移。
// 生命周期语义留在实现里，不进共用模型。
type strategy interface {
	// validateCreate 校验创建输入并返回落库所需的派生值。
	validateCreate(ctx context.Context, c *ent.Client, in NewRelease) (*createPlan, error)
	// action 执行一个策略动作，产出对版本的 allocation 与发布状态变更。
	action(ctx context.Context, c *ent.Client, rel *Release, act string, arg any) (*actionResult, error)
}

// createPlan 是 strategy 解析创建输入后的落库计划（统一模型列）。
type createPlan struct {
	Strategy           Strategy
	DeploymentID       uuid.UUID
	ServiceID          *uuid.UUID
	Name               string
	Phase              Phase
	PrimaryVersionID   uuid.UUID
	SecondaryVersionID uuid.UUID
	PrimaryWeight      int
	SecondaryWeight    int
	PreviousPrimaryID  *uuid.UUID
	Config             json.RawMessage
	// allocations 是创建后立即要写入的版本分配（canary 创建时不分配，蓝绿创建时即翻转）。
	allocations []allocation
}

// actionResult 是一次动作的产出。
type actionResult struct {
	Phase       Phase
	PrimaryWeight   int
	SecondaryWeight int
	SetStart    bool
	SetFinish   bool
	allocations []allocation
}

// ---------------------------------------------------------------- canary

type canaryStrategy struct{}

func (canaryStrategy) validateCreate(ctx context.Context, c *ent.Client, in NewRelease) (*createPlan, error) {
	if in.PrimaryVersionID == uuid.Nil || in.SecondaryVersionID == uuid.Nil {
		return nil, pkg.ErrValidation("canary 需要 stable 与 canary 两个版本")
	}
	if in.PrimaryVersionID == in.SecondaryVersionID {
		return nil, pkg.ErrValidation("stable 与 canary 版本不能相同")
	}
	_, _, _, sDep, err := versionRef(ctx, c, in.PrimaryVersionID)
	if err != nil {
		return nil, err
	}
	_, _, _, cDep, err := versionRef(ctx, c, in.SecondaryVersionID)
	if err != nil {
		return nil, err
	}
	if sDep != cDep {
		return nil, pkg.ErrValidation("stable 与 canary 版本必须属于同一部署")
	}
	if in.TargetWeight == 0 {
		in.TargetWeight = 100
	}
	if in.StepWeight == 0 {
		in.StepWeight = 10
	}
	cfg, _ := json.Marshal(CanaryConfig{TargetWeight: in.TargetWeight, StepWeight: in.StepWeight})
	// 创建时只登记，不切流量（canary 权重 0，stable 保持 100）。
	return &createPlan{
		Strategy:           StrategyCanary,
		DeploymentID:       sDep,
		ServiceID:          in.ServiceID,
		Name:               in.Name,
		Phase:              PhaseCreated,
		PrimaryVersionID:   in.PrimaryVersionID, // stable
		SecondaryVersionID: in.SecondaryVersionID, // canary
		PrimaryWeight:      100,
		SecondaryWeight:    0,
		Config:             cfg,
	}, nil
}

func (s canaryStrategy) action(ctx context.Context, c *ent.Client, rel *Release, act string, arg any) (*actionResult, error) {
	switch act {
	case string(ActionStart):
		return s.start(ctx, c, rel)
	case string(ActionPause):
		return s.pause(rel)
	case string(ActionResume):
		return s.resume(rel)
	case string(ActionWeight):
		return s.setWeight(rel, arg)
	case string(ActionPromote):
		return s.promote(rel)
	case string(ActionRollback):
		return s.rollback(rel)
	default:
		return nil, pkg.ErrValidation("canary 不支持动作: " + act)
	}
}

// start：canary 权重设为 initial(默认10)，stable 自动 100-w。
func (canaryStrategy) start(ctx context.Context, c *ent.Client, rel *Release) (*actionResult, error) {
	switch rel.Phase {
	case PhaseCreated, PhasePaused, PhaseRolledBack:
	default:
		return nil, pkg.ErrConflict("发布当前状态不可 start（phase=" + string(rel.Phase) + ")")
	}
	// 基线校验：stable 当前必须是 100%（不能叠在已有分流之上）。
	sw, _, _, _, err := versionRef(ctx, c, rel.PrimaryVersionID)
	if err != nil {
		return nil, err
	}
	if sw != 100 {
		return nil, pkg.ErrValidation("stable 版本当前权重非 100%（存在其它分流），无法作为基线")
	}
	w := rel.SecondaryWeight
	if w == 0 {
		w = 10
	}
	return &actionResult{
		Phase:           PhaseRunning,
		PrimaryWeight:   100 - w,
		SecondaryWeight: w,
		SetStart:        true,
		allocations: []allocation{
			{VersionID: rel.SecondaryVersionID, Weight: w, Status: "canary"},
			{VersionID: rel.PrimaryVersionID, Weight: 100 - w, Status: "stable"},
		},
	}, nil
}

func (canaryStrategy) pause(rel *Release) (*actionResult, error) {
	if rel.Phase != PhaseRunning {
		return nil, pkg.ErrConflict("仅 running 状态可 pause")
	}
	// 冻结权重，不改分配。
	return &actionResult{Phase: PhasePaused, PrimaryWeight: rel.PrimaryWeight, SecondaryWeight: rel.SecondaryWeight}, nil
}

func (canaryStrategy) resume(rel *Release) (*actionResult, error) {
	if rel.Phase != PhasePaused {
		return nil, pkg.ErrConflict("仅 paused 状态可 resume")
	}
	return &actionResult{Phase: PhaseRunning, PrimaryWeight: rel.PrimaryWeight, SecondaryWeight: rel.SecondaryWeight}, nil
}

func (canaryStrategy) setWeight(rel *Release, arg any) (*actionResult, error) {
	w, _ := arg.(int)
	if w < 0 || w > 100 {
		return nil, pkg.ErrValidation("weight 须在 0-100")
	}
	if rel.Phase != PhaseRunning && rel.Phase != PhasePaused {
		return nil, pkg.ErrConflict("仅 running/paused 状态可调权重")
	}
	return &actionResult{
		Phase:           rel.Phase,
		PrimaryWeight:   100 - w,
		SecondaryWeight: w,
		allocations: []allocation{
			{VersionID: rel.SecondaryVersionID, Weight: w, Status: "canary"},
			{VersionID: rel.PrimaryVersionID, Weight: 100 - w, Status: "stable"},
		},
	}, nil
}

// promote：canary → stable(100)，原 stable → draining(0)。
func (canaryStrategy) promote(rel *Release) (*actionResult, error) {
	if rel.Phase != PhaseRunning && rel.Phase != PhasePaused {
		return nil, pkg.ErrConflict("仅 running/paused 状态可 promote")
	}
	return &actionResult{
		Phase:           PhaseCompleted,
		PrimaryWeight:   0,
		SecondaryWeight: 100,
		SetFinish:       true,
		allocations: []allocation{
			{VersionID: rel.SecondaryVersionID, Weight: 100, Status: "stable"},
			{VersionID: rel.PrimaryVersionID, Weight: 0, Status: "draining"},
		},
	}, nil
}

// rollback：canary 排空(0→standby)，原 stable 恢复 100。
func (canaryStrategy) rollback(rel *Release) (*actionResult, error) {
	switch rel.Phase {
	case PhaseRunning, PhasePaused, PhaseCreated:
	default:
		return nil, pkg.ErrConflict("当前状态不可 rollback（phase=" + string(rel.Phase) + ")")
	}
	return &actionResult{
		Phase:           PhaseRolledBack,
		PrimaryWeight:   100,
		SecondaryWeight: 0,
		SetFinish:       true,
		allocations: []allocation{
			{VersionID: rel.SecondaryVersionID, Weight: 0, Status: "standby"},
			{VersionID: rel.PrimaryVersionID, Weight: 100, Status: "stable"},
		},
	}, nil
}

// ---------------------------------------------------------------- bluegreen

type blueGreenStrategy struct{}

func (blueGreenStrategy) validateCreate(ctx context.Context, c *ent.Client, in NewRelease) (*createPlan, error) {
	blue, green := in.BlueVersionID, in.GreenVersionID
	if blue == uuid.Nil || green == uuid.Nil {
		return nil, pkg.ErrValidation("bluegreen 需要 blue 与 green 两个版本")
	}
	if blue == green {
		return nil, pkg.ErrValidation("blue 与 green 版本不能相同")
	}
	blueDep, err := versionDeployment(ctx, c, blue)
	if err != nil {
		return nil, err
	}
	greenDep, err := versionDeployment(ctx, c, green)
	if err != nil {
		return nil, err
	}
	if blueDep != greenDep {
		return nil, pkg.ErrValidation("blue 与 green 版本必须属于同一部署")
	}
	if in.DeploymentID != uuid.Nil && in.DeploymentID != blueDep {
		return nil, pkg.ErrValidation("deployment_id 与版本归属不一致")
	}
	// 初始 active：缺省 blue。
	active := in.PrimaryVersionID
	if active == uuid.Nil || (active != blue && active != green) {
		active = blue
	}
	other := blue
	if active == blue {
		other = green
	}
	cfg, _ := json.Marshal(BlueGreenConfig{BlueVersionID: blue, GreenVersionID: green})
	// 创建即初始激活：active 100 + active 状态，另一色 0 + standby。
	return &createPlan{
		Strategy:           StrategyBlueGreen,
		DeploymentID:       blueDep,
		ServiceID:          in.ServiceID,
		Name:               defaultName(in.Name, "blue-green"),
		Phase:              PhaseActive,
		PrimaryVersionID:   active,
		SecondaryVersionID: other,
		PrimaryWeight:      100,
		SecondaryWeight:    0,
		Config:             cfg,
		allocations: []allocation{
			{VersionID: active, Weight: 100, Status: "active"},
			{VersionID: other, Weight: 0, Status: "standby"},
		},
	}, nil
}

func (s blueGreenStrategy) action(ctx context.Context, c *ent.Client, rel *Release, act string, arg any) (*actionResult, error) {
	switch act {
	case string(ActionSwitch):
		return s.switchActive(rel, arg)
	case string(ActionRollback):
		return s.rollback(rel)
	default:
		return nil, pkg.ErrValidation("bluegreen 不支持动作: " + act)
	}
}

// switchActive：把 active 切到 target，旧 active 记入 previous。
func (blueGreenStrategy) switchActive(rel *Release, arg any) (*actionResult, error) {
	target, _ := arg.(uuid.UUID)
	if target == uuid.Nil {
		return nil, pkg.ErrValidation("target_version_id 必填")
	}
	if target != rel.PrimaryVersionID && target != rel.SecondaryVersionID {
		return nil, pkg.ErrValidation("target 必须是该发布的 blue 或 green 版本")
	}
	if target == rel.PrimaryVersionID {
		return nil, pkg.ErrValidation("target 已是当前 active 版本")
	}
	old := rel.PrimaryVersionID
	return &actionResult{
		Phase:           PhaseActive,
		PrimaryWeight:   100,
		SecondaryWeight: 0,
		allocations: []allocation{
			{VersionID: target, Weight: 100, Status: "active"},
			{VersionID: old, Weight: 0, Status: "standby"},
		},
	}, nil
}

// rollback：active 切回 previous（= 当前 secondary）。
func (blueGreenStrategy) rollback(rel *Release) (*actionResult, error) {
	if rel.PreviousPrimaryID == nil {
		return nil, pkg.ErrConflict("无上一 active 可回滚")
	}
	prev := *rel.PreviousPrimaryID
	if prev != rel.PrimaryVersionID && prev != rel.SecondaryVersionID {
		return nil, pkg.ErrConflict("上一 active 不在当前版本对内")
	}
	if prev == rel.PrimaryVersionID {
		return nil, pkg.ErrConflict("已是上一 active")
	}
	return &actionResult{
		Phase:           PhaseActive,
		PrimaryWeight:   100,
		SecondaryWeight: 0,
		allocations: []allocation{
			{VersionID: prev, Weight: 100, Status: "active"},
			{VersionID: rel.PrimaryVersionID, Weight: 0, Status: "standby"},
		},
	}, nil
}

// ---------------------------------------------------------------- helpers

func strategyFor(s Strategy) (strategy, error) {
	switch s {
	case StrategyCanary:
		return canaryStrategy{}, nil
	case StrategyBlueGreen:
		return blueGreenStrategy{}, nil
	default:
		return nil, pkg.ErrValidation("未知发布策略: " + string(s))
	}
}

func parseCanaryConfig(raw json.RawMessage) CanaryConfig {
	var c CanaryConfig
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &c)
	}
	if c.TargetWeight == 0 {
		c.TargetWeight = 100
	}
	if c.StepWeight == 0 {
		c.StepWeight = 10
	}
	return c
}

func defaultName(name, fallback string) string {
	if name == "" {
		return fallback
	}
	return name
}

// versionRef 事务内读取版本（weight/status/version 名/部署）。
func versionRef(ctx context.Context, c *ent.Client, id uuid.UUID) (weight int, status string, version string, deploymentID uuid.UUID, err error) {
	v, err := c.DeploymentVersion.Get(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return 0, "", "", uuid.Nil, pkg.ErrNotFound("版本不存在")
		}
		return 0, "", "", uuid.Nil, err
	}
	return v.Weight, v.Status, v.Version, v.DeploymentID, nil
}

// versionDeployment 查询版本所属 deployment。
func versionDeployment(ctx context.Context, c *ent.Client, id uuid.UUID) (uuid.UUID, error) {
	v, err := c.DeploymentVersion.Get(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return uuid.Nil, pkg.ErrValidation("版本不存在: " + id.String())
		}
		return uuid.Nil, err
	}
	return v.DeploymentID, nil
}
