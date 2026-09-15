package release

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func mustCanary(t *testing.T, phase Phase, primaryW, secondaryW int) *Release {
	t.Helper()
	return &Release{
		ID:                 uuid.New(),
		Strategy:           StrategyCanary,
		Phase:              phase,
		PrimaryVersionID:   uuid.New(),
		SecondaryVersionID: uuid.New(),
		PrimaryWeight:      primaryW,
		SecondaryWeight:    secondaryW,
		Config:             json.RawMessage(`{"target_weight":100,"step_weight":10}`),
	}
}

func allocationFor(res *actionResult, id uuid.UUID) (allocation, bool) {
	for _, a := range res.allocations {
		if a.VersionID == id {
			return a, true
		}
	}
	return allocation{}, false
}

// ---- canary 状态机 ----

func TestCanary_PauseResume(t *testing.T) {
	rel := mustCanary(t, PhaseRunning, 90, 10)

	res, err := canaryStrategy{}.pause(rel)
	if err != nil {
		t.Fatalf("pause: %v", err)
	}
	if res.Phase != PhasePaused {
		t.Fatalf("pause phase = %s, want paused", res.Phase)
	}
	// pause 只冻结，不改变分配。
	if res.PrimaryWeight != 90 || res.SecondaryWeight != 10 {
		t.Fatalf("pause changed weights: %d/%d", res.PrimaryWeight, res.SecondaryWeight)
	}
	if len(res.allocations) != 0 {
		t.Fatalf("pause should not allocate, got %d", len(res.allocations))
	}

	// resume 要求当前为 paused：用 paused 状态的记录测。
	res, err = canaryStrategy{}.resume(mustCanary(t, PhasePaused, 90, 10))
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if res.Phase != PhaseRunning {
		t.Fatalf("resume phase = %s, want running", res.Phase)
	}
}

func TestCanary_PauseRequiresRunning(t *testing.T) {
	rel := mustCanary(t, PhaseCreated, 100, 0)
	if _, err := (canaryStrategy{}).pause(rel); err == nil {
		t.Fatal("pause on created should error")
	}
}

func TestCanary_SetWeight(t *testing.T) {
	rel := mustCanary(t, PhaseRunning, 90, 10)
	res, err := canaryStrategy{}.setWeight(rel, 40)
	if err != nil {
		t.Fatalf("setWeight: %v", err)
	}
	// canary 权重落 secondary，stable 自动补足到 100。
	if res.SecondaryWeight != 40 || res.PrimaryWeight != 60 {
		t.Fatalf("weights = %d/%d, want 60/40", res.PrimaryWeight, res.SecondaryWeight)
	}
	sec, ok := allocationFor(res, rel.SecondaryVersionID)
	if !ok || sec.Weight != 40 || sec.Status != "canary" {
		t.Fatalf("secondary allocation = %+v", sec)
	}
	pri, ok := allocationFor(res, rel.PrimaryVersionID)
	if !ok || pri.Weight != 60 || pri.Status != "stable" {
		t.Fatalf("primary allocation = %+v", pri)
	}
}

func TestCanary_SetWeightRange(t *testing.T) {
	rel := mustCanary(t, PhaseRunning, 90, 10)
	if _, err := (canaryStrategy{}).setWeight(rel, -1); err == nil {
		t.Fatal("weight -1 should error")
	}
	if _, err := (canaryStrategy{}).setWeight(rel, 101); err == nil {
		t.Fatal("weight 101 should error")
	}
	// created 阶段不允许调权重。
	if _, err := (canaryStrategy{}).setWeight(mustCanary(t, PhaseCreated, 100, 0), 50); err == nil {
		t.Fatal("setWeight on created should error")
	}
}

func TestCanary_Promote(t *testing.T) {
	rel := mustCanary(t, PhaseRunning, 70, 30)
	res, err := canaryStrategy{}.promote(rel)
	if err != nil {
		t.Fatalf("promote: %v", err)
	}
	if res.Phase != PhaseCompleted || !res.SetFinish {
		t.Fatalf("promote phase=%s finish=%v", res.Phase, res.SetFinish)
	}
	// canary 转正：secondary → stable 100；原 stable → draining 0。
	sec, _ := allocationFor(res, rel.SecondaryVersionID)
	if sec.Weight != 100 || sec.Status != "stable" {
		t.Fatalf("promoted secondary = %+v", sec)
	}
	pri, _ := allocationFor(res, rel.PrimaryVersionID)
	if pri.Weight != 0 || pri.Status != "draining" {
		t.Fatalf("old primary = %+v", pri)
	}
}

func TestCanary_Rollback(t *testing.T) {
	rel := mustCanary(t, PhaseRunning, 60, 40)
	res, err := canaryStrategy{}.rollback(rel)
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if res.Phase != PhaseRolledBack || !res.SetFinish {
		t.Fatalf("rollback phase=%s finish=%v", res.Phase, res.SetFinish)
	}
	sec, _ := allocationFor(res, rel.SecondaryVersionID)
	if sec.Weight != 0 || sec.Status != "standby" {
		t.Fatalf("rolled-back secondary = %+v", sec)
	}
	pri, _ := allocationFor(res, rel.PrimaryVersionID)
	if pri.Weight != 100 || pri.Status != "stable" {
		t.Fatalf("restored primary = %+v", pri)
	}
}

func TestCanary_UnsupportedAction(t *testing.T) {
	rel := mustCanary(t, PhaseRunning, 90, 10)
	// canary 不认 switch 动作。
	if _, err := (canaryStrategy{}).action(nil, nil, rel, string(ActionSwitch), nil); err == nil {
		t.Fatal("canary should reject switch")
	}
}

// ---- bluegreen 状态机 ----

func mustBG(t *testing.T, primaryW, secondaryW int) *Release {
	t.Helper()
	return &Release{
		ID:                 uuid.New(),
		Strategy:           StrategyBlueGreen,
		Phase:              PhaseActive,
		PrimaryVersionID:   uuid.New(),
		SecondaryVersionID: uuid.New(),
		PrimaryWeight:      primaryW,
		SecondaryWeight:    secondaryW,
		Config:             json.RawMessage(`{}`),
	}
}

func TestBlueGreen_Switch(t *testing.T) {
	rel := mustBG(t, 100, 0)
	res, err := blueGreenStrategy{}.action(nil, nil, rel, string(ActionSwitch), rel.SecondaryVersionID)
	if err != nil {
		t.Fatalf("switch: %v", err)
	}
	if res.Phase != PhaseActive || res.PrimaryWeight != 100 || res.SecondaryWeight != 0 {
		t.Fatalf("switch result = %+v", res)
	}
	// 目标升 active(100)，旧 active 降 standby(0)。
	tgt, _ := allocationFor(res, rel.SecondaryVersionID)
	if tgt.Weight != 100 || tgt.Status != "active" {
		t.Fatalf("new active = %+v", tgt)
	}
	old, _ := allocationFor(res, rel.PrimaryVersionID)
	if old.Weight != 0 || old.Status != "standby" {
		t.Fatalf("old active = %+v", old)
	}
}

func TestBlueGreen_SwitchGuards(t *testing.T) {
	rel := mustBG(t, 100, 0)
	// 切到当前 active：拒绝。
	if _, err := (blueGreenStrategy{}).action(nil, nil, rel, string(ActionSwitch), rel.PrimaryVersionID); err == nil {
		t.Fatal("switch to current active should error")
	}
	// 非本对版本：拒绝。
	if _, err := (blueGreenStrategy{}).action(nil, nil, rel, string(ActionSwitch), uuid.New()); err == nil {
		t.Fatal("switch to foreign version should error")
	}
	// 空 target：拒绝。
	if _, err := (blueGreenStrategy{}).action(nil, nil, rel, string(ActionSwitch), uuid.Nil); err == nil {
		t.Fatal("switch with nil target should error")
	}
}

func TestBlueGreen_Rollback(t *testing.T) {
	rel := mustBG(t, 100, 0)
	// 无 previous：拒绝。
	if _, err := (blueGreenStrategy{}).rollback(rel); err == nil {
		t.Fatal("rollback without previous should error")
	}
	// previous = secondary（另一色）：切回。
	prev := rel.SecondaryVersionID
	rel.PreviousPrimaryID = &prev
	res, err := blueGreenStrategy{}.rollback(rel)
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	back, _ := allocationFor(res, prev)
	if back.Weight != 100 || back.Status != "active" {
		t.Fatalf("restored active = %+v", back)
	}
}

func TestBlueGreen_UnsupportedAction(t *testing.T) {
	rel := mustBG(t, 100, 0)
	// bluegreen 不认 set-weight 类动作（start/pause/resume/promote 均不支持）。
	for _, act := range []Action{ActionStart, ActionPause, ActionResume, ActionPromote, ActionWeight} {
		if _, err := (blueGreenStrategy{}).action(nil, nil, rel, string(act), nil); err == nil {
			t.Fatalf("bluegreen should reject %s", act)
		}
	}
}

// ---- 公共辅助 ----

func TestValidStrategy(t *testing.T) {
	if !ValidStrategy(StrategyCanary) || !ValidStrategy(StrategyBlueGreen) {
		t.Fatal("known strategies should be valid")
	}
	if ValidStrategy("rolling") {
		t.Fatal("unknown strategy should be invalid")
	}
}

func TestStrategyFor(t *testing.T) {
	if _, err := strategyFor(StrategyCanary); err != nil {
		t.Fatalf("canary: %v", err)
	}
	if _, err := strategyFor(StrategyBlueGreen); err != nil {
		t.Fatalf("bluegreen: %v", err)
	}
	if _, err := strategyFor("bogus"); err == nil {
		t.Fatal("unknown strategy should error")
	}
}

func TestPhaseTerminal(t *testing.T) {
	// 终态发布释放"部署级活跃名额"。
	for _, p := range []Phase{PhaseCompleted, PhaseRolledBack, PhaseFailed} {
		if !p.Terminal() {
			t.Fatalf("%s should be terminal", p)
		}
	}
	// 非终态（含 bluegreen 的 active）仍占名额 → 部署级互斥生效。
	for _, p := range []Phase{PhaseCreated, PhaseRunning, PhasePaused, PhaseActive} {
		if p.Terminal() {
			t.Fatalf("%s should not be terminal", p)
		}
	}
}

func TestParseCanaryConfig(t *testing.T) {
	// 空配置回落默认。
	c := parseCanaryConfig(nil)
	if c.TargetWeight != 100 || c.StepWeight != 10 {
		t.Fatalf("defaults = %+v", c)
	}
	// 有值则用值。
	c = parseCanaryConfig(json.RawMessage(`{"target_weight":80,"step_weight":5}`))
	if c.TargetWeight != 80 || c.StepWeight != 5 {
		t.Fatalf("parsed = %+v", c)
	}
	// 畸形 JSON 回落到默认。
	c = parseCanaryConfig(json.RawMessage(`{bad`))
	if c.TargetWeight != 100 || c.StepWeight != 10 {
		t.Fatalf("malformed should fall back, got %+v", c)
	}
}
