package loadbalancer

import (
	"testing"

	"github.com/NeoPlayful/maple-gateway/server/internal/router"
)

func lcPool() []router.PoolMember {
	return []router.PoolMember{
		{ID: "a", Endpoint: "10.0.0.1", Weight: 1},
		{ID: "b", Endpoint: "10.0.0.2", Weight: 1},
	}
}

func TestLeastConnections_Empty(t *testing.T) {
	if m := LeastConnections(nil, newLeastConnTracker()); m != nil {
		t.Fatal("expected nil for empty pool")
	}
}

func TestLeastConnections_ZeroConnectionsFirst(t *testing.T) {
	tr := newLeastConnTracker()
	pool := lcPool()
	// 全部零连接：应选第一个（或权重更高者，这里同权重选首个）。
	m := LeastConnections(pool, tr)
	if m.ID != "a" {
		t.Fatalf("expected a when all idle, got %s", m.ID)
	}
}

func TestLeastConnections_PrefersIdle(t *testing.T) {
	tr := newLeastConnTracker()
	pool := lcPool()
	// a 有 5 个在途连接，b 空闲 → 应选 b。
	tr.IncConn("a")
	tr.IncConn("a")
	tr.IncConn("a")
	tr.IncConn("a")
	tr.IncConn("a")
	if m := LeastConnections(pool, tr); m.ID != "b" {
		t.Fatalf("expected b (idle), got %s", m.ID)
	}
	// b 收到 1 连接后仍比 a(5) 少 → 仍选 b。
	tr.IncConn("b")
	if m := LeastConnections(pool, tr); m.ID != "b" {
		t.Fatalf("expected b (1 conn) over a (5), got %s", m.ID)
	}
}

func TestLeastConnections_DecClampsAtZero(t *testing.T) {
	tr := newLeastConnTracker()
	tr.DecConn("a") // 空减不应变负
	if c := tr.count("a"); c != 0 {
		t.Fatalf("count went negative: %d", c)
	}
}

func TestLeastConnections_WeightTieBreak(t *testing.T) {
	tr := newLeastConnTracker()
	pool := []router.PoolMember{
		{ID: "low", Endpoint: "10.0.0.1", Weight: 1},
		{ID: "high", Endpoint: "10.0.0.2", Weight: 3},
	}
	// 同连接数（都 0）时选权重高者。
	if m := LeastConnections(pool, tr); m.ID != "high" {
		t.Fatalf("expected high-weight on tie, got %s", m.ID)
	}
}

func TestBalancer_LeastConnectionsViaWrapper(t *testing.T) {
	b := NewBalancer()
	pool := lcPool()
	b.IncConn("a")
	b.IncConn("a")
	if m := b.SelectLeastConnections(pool); m.ID != "b" {
		t.Fatalf("expected b via balancer wrapper, got %s", m.ID)
	}
}
