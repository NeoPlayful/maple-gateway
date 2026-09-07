package loadbalancer

import (
	"fmt"
	"testing"

	"github.com/NeoPlayful/maple-gateway/server/internal/router"
)

func poolOf(weights ...int) []router.PoolMember {
	out := make([]router.PoolMember, len(weights))
	for i, w := range weights {
		out[i] = router.PoolMember{
			ID:       fmt.Sprintf("id-%d", i),
			Endpoint: fmt.Sprintf("10.0.0.%d", i+1),
			Weight:   w,
		}
	}
	return out
}

func TestRoundRobin_EqualWeights(t *testing.T) {
	b := NewBalancer()
	pool := poolOf(1, 1, 1)
	counts := map[string]int{}
	for i := 0; i < 300; i++ {
		m := b.Select("k", pool)
		counts[m.Endpoint]++
	}
	// 3 实例 RR 应基本均分。
	for _, c := range counts {
		if c < 95 || c > 105 {
			t.Fatalf("uneven RR distribution: %v", counts)
		}
	}
}

func TestWeightedRoundRobin(t *testing.T) {
	b := NewBalancer()
	// 权重 2:1 的两个实例：约 2/3 vs 1/3。
	pool := poolOf(2, 1)
	counts := map[string]int{}
	for i := 0; i < 3000; i++ {
		m := b.Select("k", pool)
		counts[m.Endpoint]++
	}
	a := counts["10.0.0.1"] // weight 2
	c := counts["10.0.0.2"] // weight 1
	if a < 1800 || a > 2200 {
		t.Fatalf("weight-2 instance got %d, want ~2000", a)
	}
	if c < 800 || c > 1200 {
		t.Fatalf("weight-1 instance got %d, want ~1000", c)
	}
}

func TestSelect_EmptyPool(t *testing.T) {
	b := NewBalancer()
	if m := b.Select("k", nil); m != nil {
		t.Fatal("expected nil for empty pool")
	}
}

func TestBalancer_PoolChangeResets(t *testing.T) {
	b := NewBalancer()
	p1 := poolOf(1)
	// 先跑一轮让状态建立。
	b.Select("k", p1)
	// 池从 1 实例变 2 实例，不应崩溃且能正常轮转。
	p2 := poolOf(1, 1)
	for i := 0; i < 10; i++ {
		if b.Select("k", p2) == nil {
			t.Fatal("select on new pool returned nil")
		}
	}
}

func TestWeightChangeResetsDistribution(t *testing.T) {
	b := NewBalancer()
	// 先以权重 1:1 跑若干轮建立状态。
	even := poolOf(1, 1)
	for i := 0; i < 10; i++ {
		b.Select("k", even)
	}
	// 权重改为 3:1，分布应切换到约 3:1（说明指纹识别到 weight 变化）。
	weighted := []router.PoolMember{
		{ID: "id-0", Endpoint: "10.0.0.1", Weight: 3},
		{ID: "id-1", Endpoint: "10.0.0.2", Weight: 1},
	}
	counts := map[string]int{}
	for i := 0; i < 4000; i++ {
		m := b.Select("k", weighted)
		counts[m.Endpoint]++
	}
	if a := counts["10.0.0.1"]; a < 2700 || a > 3300 {
		t.Fatalf("weight-3 instance got %d, want ~3000", a)
	}
}
