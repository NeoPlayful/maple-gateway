package loadbalancer

import (
	"fmt"
	"testing"

	"github.com/NeoPlayful/maple-gateway/server/internal/router"
)

func idPool(n int) []router.PoolMember {
	out := make([]router.PoolMember, n)
	for i := 0; i < n; i++ {
		out[i] = router.PoolMember{
			ID:       fmt.Sprintf("inst-%d", i),
			Endpoint: fmt.Sprintf("10.0.0.%d", i+1),
		}
	}
	return out
}

func TestConsistentHash_SameKeySameInstance(t *testing.T) {
	pool := idPool(5)
	for i := 0; i < 50; i++ {
		key := fmt.Sprintf("user-%d", i)
		first := ConsistentHash(pool, key)
		for j := 0; j < 10; j++ {
			if got := ConsistentHash(pool, key); got.Endpoint != first.Endpoint {
				t.Fatalf("key %s drifted: %s then %s", key, first.Endpoint, got.Endpoint)
			}
		}
	}
}

func TestConsistentHash_MinimalMigration(t *testing.T) {
	base := idPool(6)
	// 固定一批 key 在 base 池上的归属。
	type mapping struct {
		key      string
		instance string
	}
	keys := make([]mapping, 0, 200)
	for i := 0; i < 200; i++ {
		key := fmt.Sprintf("sess-%d", i)
		keys = append(keys, mapping{key, ConsistentHash(base, key).Endpoint})
	}

	// 池增 1 实例（6→7）：绝大多数 key 归属不变（最小迁移）。
	grown := idPool(7)
	migrated := 0
	for _, m := range keys {
		if got := ConsistentHash(grown, m.key).Endpoint; got != m.instance {
			migrated++
		}
	}
	if migrated > 200/6+10 { // 期望约 1/7 迁移，容差 +10
		t.Fatalf("too many migrated on grow: %d/200", migrated)
	}

	// 池减 1 实例（6→5）：同样绝大多数不变。
	shrunk := idPool(5)
	migrated = 0
	for _, m := range keys {
		if got := ConsistentHash(shrunk, m.key).Endpoint; got != m.instance {
			migrated++
		}
	}
	if migrated > 200/5+10 {
		t.Fatalf("too many migrated on shrink: %d/200", migrated)
	}
}

func TestConsistentHash_EmptyPool(t *testing.T) {
	if m := ConsistentHash(nil, "k"); m != nil {
		t.Fatal("expected nil for empty pool")
	}
}

func TestConsistentHash_NoEndpointEmptyID(t *testing.T) {
	// ID 为空时用 Endpoint 兜底：同 Endpoint 仍应稳定。
	pool := []router.PoolMember{
		{Endpoint: "10.0.0.1"}, {Endpoint: "10.0.0.2"}, {Endpoint: "10.0.0.3"},
	}
	first := ConsistentHash(pool, "k")
	if first == nil {
		t.Fatal("nil on non-empty pool")
	}
	for i := 0; i < 5; i++ {
		if got := ConsistentHash(pool, "k"); got.Endpoint != first.Endpoint {
			t.Fatal("key drifted with empty-ID pool")
		}
	}
}
