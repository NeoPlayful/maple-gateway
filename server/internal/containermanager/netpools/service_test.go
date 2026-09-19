package netpools

import (
	"context"
	"testing"
)

func TestServiceDeleteGuardsInUsePool(t *testing.T) {
	ctx := context.Background()
	pools := NewPoolStore(nil)
	nets := NewNetworkStore(nil)
	alloc := NewAllocator(pools, nets, nil)
	svc := NewService(pools, nets, alloc, nil)

	view, err := svc.Create(ctx, CreatePoolInput{
		NodeID: "n1", Name: "default", AddressPool: "10.128.0.0/9", ProjectPrefix: 24,
	})
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}
	// 空池可删。
	if err := svc.Delete(ctx, "n1", view.ID); err != nil {
		t.Fatalf("empty pool should be deletable: %v", err)
	}

	// 重建后占用一个槽位，再删应被拒绝（文档 §42/§66）。
	view, _ = svc.Create(ctx, CreatePoolInput{
		NodeID: "n1", Name: "default", AddressPool: "10.128.0.0/9", ProjectPrefix: 24,
	})
	if _, err := alloc.Allocate(ctx, "n1", "proj-1", "maple-1"); err != nil {
		t.Fatalf("allocate: %v", err)
	}
	if err := svc.Delete(ctx, "n1", view.ID); err == nil {
		t.Error("expected IPAM_POOL_IN_USE rejection")
	}
}

func TestServiceUpdateRejectsForeignNode(t *testing.T) {
	ctx := context.Background()
	pools := NewPoolStore(nil)
	svc := NewService(pools, NewNetworkStore(nil), nil, nil)
	view, _ := svc.Create(ctx, CreatePoolInput{NodeID: "n1", Name: "p", AddressPool: "10.128.0.0/9", ProjectPrefix: 24})

	if _, err := svc.Update(ctx, "n2", view.ID, UpdatePoolInput{Name: "x"}); err == nil {
		t.Error("update on wrong node should fail")
	}
	p := 50
	got, err := svc.Update(ctx, "n1", view.ID, UpdatePoolInput{Priority: &p})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if got.Priority != 50 {
		t.Errorf("priority not updated: %d", got.Priority)
	}
}

func TestServiceEnsureDefaultForNodeIdempotent(t *testing.T) {
	ctx := context.Background()
	pools := NewPoolStore(nil)
	svc := NewService(pools, NewNetworkStore(nil), nil, nil)

	if err := svc.EnsureDefaultForNode(ctx, "n1"); err != nil {
		t.Fatalf("ensure default: %v", err)
	}
	if err := svc.EnsureDefaultForNode(ctx, "n1"); err != nil {
		t.Fatalf("re-ensure default: %v", err)
	}
	if got := svc.List("n1"); len(got) != 1 {
		t.Errorf("expected exactly 1 default pool, got %d", len(got))
	}
}

func TestServiceViewUsage(t *testing.T) {
	ctx := context.Background()
	pools := NewPoolStore(nil)
	nets := NewNetworkStore(nil)
	alloc := NewAllocator(pools, nets, nil)
	svc := NewService(pools, nets, alloc, nil)

	view, _ := svc.Create(ctx, CreatePoolInput{NodeID: "n1", Name: "p", AddressPool: "10.128.0.0/24", ProjectPrefix: 28})
	// /24 → /28 容量 16。
	if view.Capacity != 16 {
		t.Errorf("capacity = %d, want 16", view.Capacity)
	}
	if _, err := alloc.Allocate(ctx, "n1", "proj-1", "maple-1"); err != nil {
		t.Fatalf("allocate: %v", err)
	}
	// 分配后 reserved=1（allocator 置 reserved），可用应减 1。
	views := svc.List("n1")
	if len(views) != 1 {
		t.Fatalf("expected 1 pool")
	}
	if views[0].Reserved != 1 {
		t.Errorf("reserved = %d, want 1", views[0].Reserved)
	}
	if views[0].Available != 15 {
		t.Errorf("available = %d, want 15", views[0].Available)
	}
}
