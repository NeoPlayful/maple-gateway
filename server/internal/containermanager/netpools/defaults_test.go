package netpools

import (
	"context"
	"testing"
)

// 默认池应应用系统默认的复用开关与冷却时间（Create 统一置启用，EnsureDefaultForNode 再按默认覆盖）。
func TestEnsureDefaultForNodeAppliesReuseDefaults(t *testing.T) {
	ctx := context.Background()
	pools := NewPoolStore(nil)
	svc := NewService(pools, NewNetworkStore(nil), nil, nil)
	svc.SetDefaultPool(DefaultPoolConfig{
		AddressPool: "10.128.0.0/9", ProjectPrefix: 24,
		ReuseEnabled: false, ReuseDelaySeconds: 120,
	})

	if err := svc.EnsureDefaultForNode(ctx, "n1"); err != nil {
		t.Fatalf("ensure default: %v", err)
	}
	views := svc.List("n1")
	if len(views) != 1 {
		t.Fatalf("expected 1 pool, got %d", len(views))
	}
	if views[0].AddressPool != "10.128.0.0/9" || views[0].ProjectPrefix != 24 {
		t.Errorf("pool cidr/prefix = %s/%d, want 10.128.0.0/9/24", views[0].AddressPool, views[0].ProjectPrefix)
	}
	if views[0].ReuseEnabled {
		t.Error("reuse should follow system default (false)")
	}
	if views[0].ReuseDelaySeconds != 120 {
		t.Errorf("reuse delay = %d, want 120", views[0].ReuseDelaySeconds)
	}
	if !views[0].IsSystemDefault {
		t.Error("default pool should be flagged is_system_default")
	}
}

// LoadSystemDefaultPool 在无 DB 时回退内建默认。
func TestLoadSystemDefaultPoolFallsBackWithoutDB(t *testing.T) {
	got := LoadSystemDefaultPool(context.Background(), nil)
	want := DefaultSystemPool()
	if got != want {
		t.Errorf("fallback = %+v, want %+v", got, want)
	}
}
