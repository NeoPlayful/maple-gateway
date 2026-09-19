package netpools

import (
	"context"
	"testing"
	"time"
)

func TestPoolStoreCreateRejectsOverlapOnSameNode(t *testing.T) {
	ctx := context.Background()
	s := NewPoolStore(nil)

	if _, err := s.Create(ctx, Pool{NodeID: "n1", Name: "default", AddressPool: "10.128.0.0/9", ProjectPrefix: 24}); err != nil {
		t.Fatalf("create first pool: %v", err)
	}
	// 同节点重叠必须拒绝（文档 §37）。
	if _, err := s.Create(ctx, Pool{NodeID: "n1", Name: "overlap", AddressPool: "10.128.0.0/10", ProjectPrefix: 24}); err == nil {
		t.Error("expected overlap rejection on same node")
	}
	// 不同节点允许相同 CIDR（文档 §5/§83）。
	if _, err := s.Create(ctx, Pool{NodeID: "n2", Name: "default", AddressPool: "10.128.0.0/9", ProjectPrefix: 24}); err != nil {
		t.Errorf("same CIDR on different node should be allowed: %v", err)
	}
	// 同节点不重叠的扩容池允许（文档 §7）。
	if _, err := s.Create(ctx, Pool{NodeID: "n1", Name: "expansion", AddressPool: "10.64.0.0/10", ProjectPrefix: 24, Priority: 20}); err != nil {
		t.Errorf("non-overlapping expansion pool should be allowed: %v", err)
	}
}

func TestPoolStoreRejectsReservedRange(t *testing.T) {
	ctx := context.Background()
	s := NewPoolStore(nil)
	if _, err := s.Create(ctx, Pool{NodeID: "n1", Name: "bad", AddressPool: "127.0.0.0/8", ProjectPrefix: 24}); err == nil {
		t.Error("expected reserved-range rejection")
	}
}

func TestPoolStoreListByNodePriorityOrder(t *testing.T) {
	ctx := context.Background()
	s := NewPoolStore(nil)
	a, _ := s.Create(ctx, Pool{NodeID: "n1", Name: "a", AddressPool: "10.128.0.0/9", ProjectPrefix: 24, Priority: 20})
	b, _ := s.Create(ctx, Pool{NodeID: "n1", Name: "b", AddressPool: "10.64.0.0/10", ProjectPrefix: 24, Priority: 10})

	got := s.ListByNode("n1")
	if len(got) != 2 {
		t.Fatalf("expected 2 pools, got %d", len(got))
	}
	// priority ASC：b(10) 先于 a(20)，与创建顺序无关（文档 §79）。
	if got[0].ID != b.ID || got[1].ID != a.ID {
		t.Errorf("priority order wrong: got %s,%s", got[0].Name, got[1].Name)
	}
}

func TestNetworkStoreInUseOnNode(t *testing.T) {
	ctx := context.Background()
	s := NewNetworkStore(nil)
	// 已过冷却期的 released 视为空闲。
	past := time.Now().Add(-time.Minute)
	if _, err := s.Put(ctx, ProjectNetwork{
		ProjectID: "p1", NodeID: "n1", PoolID: "pool", Subnet: "10.128.0.0/24",
		Status: NetReleased, ReuseAfter: past,
	}); err != nil {
		t.Fatalf("put released: %v", err)
	}
	if s.InUseOnNode("n1", "10.128.0.0/24") {
		t.Error("expired released subnet should be free")
	}
	// 未过冷却期的 released 仍占用。
	future := time.Now().Add(time.Minute)
	if _, err := s.Put(ctx, ProjectNetwork{
		ProjectID: "p2", NodeID: "n1", PoolID: "pool", Subnet: "10.128.1.0/24",
		Status: NetReleased, ReuseAfter: future,
	}); err != nil {
		t.Fatalf("put cooling: %v", err)
	}
	if !s.InUseOnNode("n1", "10.128.1.0/24") {
		t.Error("cooling released subnet should still be in use")
	}
	// 另一节点同子网互不影响（Node-local）。
	if s.InUseOnNode("n2", "10.128.0.0/24") {
		t.Error("different node should not be affected")
	}
}
