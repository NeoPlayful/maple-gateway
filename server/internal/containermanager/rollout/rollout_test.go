package rollout

import (
	"testing"

	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/desired"
	"github.com/google/uuid"
)

// 滚动发布：新版本从 0→2（surge），旧版本从 2→0（drain），surge 必须排在 drain 之前。
func TestPlanSurgeBeforeDrain(t *testing.T) {
	dep := uuid.New()
	old := desired.State{DeploymentID: dep, VersionID: uuid.New(), Version: "v1", Status: "standby", Replicas: 0}
	newv := desired.State{DeploymentID: dep, VersionID: uuid.New(), Version: "v2", Status: "stable", Replicas: 2}

	actual := Actual{
		old.VersionID.String(): 2,
		newv.VersionID.String(): 0,
	}
	ops := Plan([]desired.State{old, newv}, actual)

	if len(ops) != 2 {
		t.Fatalf("ops = %d, want 2", len(ops))
	}
	if ops[0].Kind != KindSurge || ops[0].State.VersionID != newv.VersionID {
		t.Errorf("first op should be surge of new version, got %s %s", ops[0].Kind, ops[0].State.Version)
	}
	if ops[1].Kind != KindDrain || ops[1].State.VersionID != old.VersionID {
		t.Errorf("second op should be drain of old version, got %s %s", ops[1].Kind, ops[1].State.Version)
	}
	if ops[0].Count != 2 || ops[1].Count != 2 {
		t.Errorf("counts = %d/%d, want 2/2", ops[0].Count, ops[1].Count)
	}
}

// 金丝雀：canary 版本按目标扩，旧版本保持不变。
func TestPlanCanaryOnlyScalesCanary(t *testing.T) {
	dep := uuid.New()
	stable := desired.State{DeploymentID: dep, VersionID: uuid.New(), Version: "v1", Status: "stable", Replicas: 5}
	canary := desired.State{DeploymentID: dep, VersionID: uuid.New(), Version: "v2", Status: "canary", Replicas: 1}

	actual := Actual{
		stable.VersionID.String(): 5,
		canary.VersionID.String(): 0,
	}
	ops := Plan([]desired.State{stable, canary}, actual)

	if len(ops) != 1 {
		t.Fatalf("ops = %d, want 1 (only canary surge)", len(ops))
	}
	if ops[0].Kind != KindSurge || ops[0].State.VersionID != canary.VersionID || ops[0].Count != 1 {
		t.Errorf("unexpected op: %+v", ops[0])
	}
}

// 一致时无操作。
func TestPlanNoOpWhenConverged(t *testing.T) {
	st := desired.State{DeploymentID: uuid.New(), VersionID: uuid.New(), Replicas: 3}
	ops := Plan([]desired.State{st}, Actual{st.VersionID.String(): 3})
	if len(ops) != 0 {
		t.Errorf("ops = %d, want 0", len(ops))
	}
}

func TestIsRetiring(t *testing.T) {
	for _, s := range []string{"inactive", "draining", "standby"} {
		if !IsRetiring(s) {
			t.Errorf("%s should be retiring", s)
		}
	}
	for _, s := range []string{"stable", "active", "canary"} {
		if IsRetiring(s) {
			t.Errorf("%s should not be retiring", s)
		}
	}
}
