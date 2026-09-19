package netpools

import (
	"context"
	"testing"
)

// Create 后 ListByNode 必须立刻包含新池（内存视图与库一致）。
func TestCreateThenListByNodeIncludesAll(t *testing.T) {
	ctx := context.Background()
	s := NewPoolStore(nil)
	names := []string{"default", "verify-expansion", "probe-xyz", "a1", "a3", "exp-clean"}
	pools := []Pool{
		{Name: "default", AddressPool: "10.128.0.0/9"},          // 10.128–10.255
		{Name: "verify-expansion", AddressPool: "10.64.0.0/10"}, // 10.64–10.127
		{Name: "probe-xyz", AddressPool: "10.32.0.0/11"},        // 10.32–10.63
		{Name: "a1", AddressPool: "10.16.0.0/12"},               // 10.16–10.31
		{Name: "a3", AddressPool: "10.8.0.0/13"},                // 10.8–10.15
		{Name: "exp-clean", AddressPool: "172.20.0.0/14"},       // 不重叠
	}
	for _, p := range pools {
		if _, err := s.Create(ctx, Pool{NodeID: "n1", Name: p.Name, AddressPool: p.AddressPool, ProjectPrefix: 24}); err != nil {
			t.Fatalf("create %s: %v", p.Name, err)
		}
	}
	got := s.ListByNode("n1")
	if len(got) != len(names) {
		seen := map[string]bool{}
		for _, p := range got {
			seen[p.Name] = true
		}
		t.Fatalf("ListByNode returned %d pools, want %d; seen=%v", len(got), len(names), seen)
	}
	for _, p := range got {
		if p.ID == "" {
			t.Errorf("pool %s missing ID", p.Name)
		}
	}
}
