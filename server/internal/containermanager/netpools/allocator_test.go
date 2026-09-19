package netpools

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func newTestAllocator(t *testing.T) (*Allocator, *PoolStore, *NetworkStore) {
	t.Helper()
	pools := NewPoolStore(nil)
	nets := NewNetworkStore(nil)
	return NewAllocator(pools, nets, nil), pools, nets
}

func pastTime() time.Time { return time.Now().Add(-time.Hour) }

func TestAllocateSequential(t *testing.T) {
	ctx := context.Background()
	a, pools, _ := newTestAllocator(t)
	if _, err := pools.Create(ctx, Pool{NodeID: "n1", Name: "default", AddressPool: "10.128.0.0/9", ProjectPrefix: 24}); err != nil {
		t.Fatalf("create pool: %v", err)
	}

	// 顺序分配：index 0,1,2 → 10.128.0.0/24, 10.128.1.0/24, 10.128.2.0/24。
	for i := 0; i < 3; i++ {
		pid := fmt.Sprintf("proj-%d", i)
		want := fmt.Sprintf("10.128.%d.0/24", i)
		n, err := a.Allocate(ctx, "n1", pid, "maple-"+pid)
		if err != nil {
			t.Fatalf("allocate %d: %v", i, err)
		}
		if n.Subnet != want {
			t.Errorf("alloc %d subnet = %s, want %s", i, n.Subnet, want)
		}
		if n.Gateway != fmt.Sprintf("10.128.%d.1", i) {
			t.Errorf("alloc %d gateway = %s", i, n.Gateway)
		}
		if n.Status != NetReserved {
			t.Errorf("alloc %d status = %s, want reserved", i, n.Status)
		}
	}
}

func TestAllocateIdempotent(t *testing.T) {
	ctx := context.Background()
	a, pools, _ := newTestAllocator(t)
	pools.Create(ctx, Pool{NodeID: "n1", Name: "default", AddressPool: "10.128.0.0/9", ProjectPrefix: 24})

	first, err := a.Allocate(ctx, "n1", "proj-x", "maple-x")
	if err != nil {
		t.Fatalf("first allocate: %v", err)
	}
	// 重新部署复用原网段，不重新申请（文档 §50）。
	again, err := a.Allocate(ctx, "n1", "proj-x", "maple-x")
	if err != nil {
		t.Fatalf("re-allocate: %v", err)
	}
	if again.Subnet != first.Subnet {
		t.Errorf("idempotent allocate changed subnet: %s -> %s", first.Subnet, again.Subnet)
	}
}

func TestAllocateFallsToNextPool(t *testing.T) {
	ctx := context.Background()
	a, pools, _ := newTestAllocator(t)
	// 池 1 极小：/29 池 → /30 子网，容量 2。
	p1, err := pools.Create(ctx, Pool{NodeID: "n1", Name: "tiny", AddressPool: "10.128.0.0/29", ProjectPrefix: 30, Priority: 10})
	if err != nil {
		t.Fatalf("create tiny pool: %v", err)
	}
	// 池 2 正常。
	p2, err := pools.Create(ctx, Pool{NodeID: "n1", Name: "expand", AddressPool: "10.64.0.0/10", ProjectPrefix: 24, Priority: 20})
	if err != nil {
		t.Fatalf("create expansion pool: %v", err)
	}

	// 填满池 1 的两个槽位。
	for i := 0; i < 2; i++ {
		if _, err := a.Allocate(ctx, "n1", fmt.Sprintf("tiny-%d", i), "maple-tiny"); err != nil {
			t.Fatalf("alloc %d in tiny pool: %v", i, err)
		}
	}
	// 池 1 耗尽 → 自动切到池 2（文档 §78）。
	n, err := a.Allocate(ctx, "n1", "p2", "maple-p2")
	if err != nil {
		t.Fatalf("alloc should fall through to pool 2: %v", err)
	}
	if n.PoolID != p2.ID {
		t.Errorf("expected allocation in expansion pool, got %s", n.Subnet)
	}
	// 池 1 应被标记 exhausted。
	if got, _ := pools.Get(p1.ID); got.Status != StatusExhausted {
		t.Errorf("tiny pool status = %s, want exhausted", got.Status)
	}
}

func TestAllocateAllPoolsExhausted(t *testing.T) {
	ctx := context.Background()
	a, pools, _ := newTestAllocator(t)
	// 单个 /29 池，容量 2。
	if _, err := pools.Create(ctx, Pool{NodeID: "n1", Name: "tiny", AddressPool: "10.128.0.0/29", ProjectPrefix: 30}); err != nil {
		t.Fatalf("create pool: %v", err)
	}
	for i := 0; i < 2; i++ {
		if _, err := a.Allocate(ctx, "n1", fmt.Sprintf("p%d", i), "maple"); err != nil {
			t.Fatalf("alloc %d: %v", i, err)
		}
	}
	_, err := a.Allocate(ctx, "n1", "p2", "maple-p2")
	if err == nil {
		t.Fatal("expected all-pools-exhausted error")
	}
	var ae *AllocError
	if !errors.As(err, &ae) || ae.Code != CodeAllPoolsExhausted {
		t.Errorf("expected %s, got %v", CodeAllPoolsExhausted, err)
	}
}

// 并发分配同一池：不得出现重复子网（文档 §84 的最小验证）。
func TestConcurrentAllocateNoDuplicate(t *testing.T) {
	ctx := context.Background()
	a, pools, _ := newTestAllocator(t)
	if _, err := pools.Create(ctx, Pool{NodeID: "n1", Name: "default", AddressPool: "10.128.0.0/24", ProjectPrefix: 28}); err != nil {
		t.Fatalf("create pool: %v", err)
	}
	// /24 池 → /28 子网，容量 16。
	const n = 16
	got := make(chan string, n)
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			pid := fmt.Sprintf("p%d", i)
			rec, err := a.Allocate(ctx, "n1", pid, "maple-"+pid)
			if err != nil {
				errs <- err
				return
			}
			got <- rec.Subnet
		}(i)
	}
	seen := map[string]bool{}
	for i := 0; i < n; i++ {
		select {
		case s := <-got:
			if seen[s] {
				t.Errorf("duplicate subnet allocated: %s", s)
			}
			seen[s] = true
		case err := <-errs:
			t.Fatalf("concurrent allocate: %v", err)
		}
	}
	if len(seen) != n {
		t.Errorf("expected %d distinct subnets, got %d", n, len(seen))
	}
}

func TestReleaseAndReuseDelay(t *testing.T) {
	ctx := context.Background()
	a, pools, _ := newTestAllocator(t)
	if _, err := pools.Create(ctx, Pool{NodeID: "n1", Name: "default", AddressPool: "10.128.0.0/29", ProjectPrefix: 30}); err != nil {
		t.Fatalf("create pool: %v", err)
	}

	first, err := a.Allocate(ctx, "n1", "p1", "maple-p1")
	if err != nil {
		t.Fatalf("alloc p1: %v", err)
	}
	if _, err := a.Allocate(ctx, "n1", "p2", "maple-p2"); err != nil {
		t.Fatalf("alloc p2: %v", err)
	}
	// 释放 p1 并设置冷却期：其槽位在冷却中仍算占用，池已满。
	if err := a.Release(ctx, "p1", 600); err != nil {
		t.Fatalf("release: %v", err)
	}
	if _, err := a.Allocate(ctx, "n1", "p3", "maple-p3"); err == nil {
		t.Error("expected exhaustion while subnet is cooling down")
	}
	// 把冷却时间调到过去，模拟已过冷却期 → 该槽位可复用（文档 §24/§86）。
	if err := a.nets.SetStatus(ctx, "p1", NetReleased, func(pn *ProjectNetwork) {
		pn.ReuseAfter = pastTime()
	}); err != nil {
		t.Fatalf("expire cooling: %v", err)
	}
	reused, err := a.Allocate(ctx, "n1", "p3", "maple-p3")
	if err != nil {
		t.Fatalf("alloc after cooling: %v", err)
	}
	if reused.Subnet != first.Subnet {
		t.Errorf("expected reuse of %s, got %s", first.Subnet, reused.Subnet)
	}
}
