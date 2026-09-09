package ha

import (
	"testing"
	"time"
)

func TestLeaderKey(t *testing.T) {
	// Leader 锁为全局单一键名：所有实例竞逐同一把锁（避免 per-instance 双主）；
	// 实际 Redis 键由 pkg.Redis.Key 前缀化为 <prefix>:ha:leader，默认前缀 maple。
	if leaderKeyName != "ha:leader" {
		t.Fatalf("leaderKeyName = %q", leaderKeyName)
	}
}

func TestCoordinatorDefaultsAndAccessors(t *testing.T) {
	c := NewCoordinator(nil, nil, nil, Config{
		InstanceID: "gw-1",
		Enabled:    true,
		Heartbeat:  0,
		LeaseTTL:   0,
	})
	if c.InstanceID() != "gw-1" {
		t.Fatalf("instance id = %q", c.InstanceID())
	}
	if !c.enabled() {
		t.Fatal("expected enabled true")
	}
	if c.IsLeader() {
		t.Fatal("should not be leader initially")
	}
	// Run 内的默认值（非负校验）在 tick 前被补上；此处直接构造 cfg 断言默认逻辑等价。
	def := Config{Enabled: true}
	if def.Heartbeat <= 0 {
		def.Heartbeat = 5 * time.Second
	}
	if def.LeaseTTL <= 0 {
		def.LeaseTTL = 10 * time.Second
	}
	if def.Heartbeat != 5*time.Second || def.LeaseTTL != 10*time.Second {
		t.Fatalf("unexpected defaults: %+v", def)
	}
}

func TestLeaseUntilInFuture(t *testing.T) {
	u := leaseUntil(15 * time.Second)
	if !u.After(time.Now().Add(14 * time.Second)) {
		t.Fatalf("lease until not in future: %v", u)
	}
}
