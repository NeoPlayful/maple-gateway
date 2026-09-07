package cache

import (
	"context"
	"testing"

	"github.com/NeoPlayful/maple-gateway/server/internal/domain"
	"github.com/NeoPlayful/maple-gateway/server/internal/instance"
	"github.com/NeoPlayful/maple-gateway/server/internal/router"
	"github.com/NeoPlayful/maple-gateway/server/internal/service"
	"github.com/NeoPlayful/maple-gateway/server/internal/tenant"
	"github.com/NeoPlayful/maple-gateway/server/internal/traffic"
	"github.com/google/uuid"
)

func vid(t *testing.T, n byte) uuid.UUID {
	t.Helper()
	id := uuid.Nil
	id[0] = 0x10
	id[1] = n
	return id
}

func TestBuildTable_NodeOfflineFiltersInstance(t *testing.T) {
	tenID := fixedID(t, 1)
	svcID := fixedID(t, 2)
	domID := fixedID(t, 3)
	nodeA := fixedID(t, 50)
	instA := fixedID(t, 40) // 挂 nodeA
	instB := fixedID(t, 41) // 无 node

	tenants := []*tenant.Tenant{{ID: tenID, Status: tenant.StatusActive}}
	services := []*service.Service{{ID: svcID, TenantID: tenID, Protocol: "http"}}
	domains := []*domain.Domain{
		{ID: domID, TenantID: tenID, Hostname: "node.example.com", ServiceID: &svcID, Status: domain.StatusActive},
	}
	insts := map[uuid.UUID][]*instance.Instance{
		svcID: {
			{ID: instA, ServiceID: svcID, NodeID: &nodeA, Address: "10.0.0.1", Port: 9101,
				Status: instance.StatusEnabled, Health: instance.HealthHealthy},
			{ID: instB, ServiceID: svcID, Address: "10.0.0.2", Port: 9102,
				Status: instance.StatusEnabled, Health: instance.HealthHealthy},
		},
	}

	// nodeA offline → 仅保留无 node 的 instB。
	table := buildTable(tenants, services, domains, insts, nil, nil,
		map[uuid.UUID]bool{nodeA: false}, nil)
	if table == nil {
		t.Fatal("expected table")
	}
	e := table.Lookup("node.example.com")
	if e == nil {
		t.Fatal("route missing")
	}
	if len(e.Pool) != 1 {
		t.Fatalf("pool = %d, want 1 (offline-node instance filtered)", len(e.Pool))
	}
	if e.Pool[0].Endpoint != "10.0.0.2:9102" {
		t.Fatalf("expected only unbound instance, got %s", e.Pool[0].Endpoint)
	}

	// nodeA online → 两个实例都在。
	table2 := buildTable(tenants, services, domains, insts, nil, nil,
		map[uuid.UUID]bool{nodeA: true}, nil)
	e2 := table2.Lookup("node.example.com")
	if len(e2.Pool) != 2 {
		t.Fatalf("pool = %d, want 2 when node online", len(e2.Pool))
	}
}

func TestResolver_StickySessionPinsInstance(t *testing.T) {
	c := newStickyVersionedCache(t)
	r := NewResolver(c)

	// 同一会话 key 恒命中同一实例（v2 池内钉住）。
	first := map[string]string{}
	for i := 0; i < 50; i++ {
		target, err := r.ResolveWith(context.Background(), "ver.example.com", router.MatchView{
			Header: map[string]string{"x-user-id": "user-42", "x-canary": "beta"},
		})
		if err != nil {
			t.Fatalf("resolve err: %v", err)
		}
		first[target.Host] = target.Host
	}
	if len(first) != 1 {
		t.Fatalf("sticky session drifted across instances: %v", first)
	}
	// 不同会话可落不同实例（v2 单实例时仍同一，故这里验证"恒单实例"即可）。
}

func TestResolver_StickyCookiePinsInstance(t *testing.T) {
	c := newStickyVersionedCache(t)
	r := NewResolver(c)

	// cookie MAPLE_SRV 会话键。
	got := map[string]bool{}
	for i := 0; i < 30; i++ {
		target, err := r.ResolveWith(context.Background(), "ver.example.com", router.MatchView{
			Header: map[string]string{"x-canary": "beta", "cookie": "MAPLE_SRV=abc123"},
		})
		if err != nil {
			t.Fatalf("resolve err: %v", err)
		}
		got[target.Host] = true
	}
	if len(got) != 1 {
		t.Fatalf("cookie sticky drifted: %v", got)
	}
}

// newStickyVersionedCache 构建带 sticky 策略的版本化 cache：v2 版本有 3 个实例供钉住。
func newStickyVersionedCache(t *testing.T) *Cache {
	t.Helper()
	tenID := fixedID(t, 1)
	svcID := fixedID(t, 2)
	domID := fixedID(t, 3)
	v1ID := vid(t, 1)
	v2ID := vid(t, 2)

	tenants := []*tenant.Tenant{{ID: tenID, Status: tenant.StatusActive}}
	services := []*service.Service{{ID: svcID, TenantID: tenID, Protocol: "http"}}
	domains := []*domain.Domain{
		{ID: domID, TenantID: tenID, Hostname: "ver.example.com", ServiceID: &svcID, Status: domain.StatusActive},
	}
	insts := map[uuid.UUID][]*instance.Instance{
		svcID: {
			{ID: fixedID(t, 40), ServiceID: svcID, VersionID: &v1ID, Address: "10.0.0.1", Port: 9101,
				Status: instance.StatusEnabled, Health: instance.HealthHealthy},
			{ID: fixedID(t, 41), ServiceID: svcID, VersionID: &v2ID, Address: "10.0.0.2", Port: 9102,
				Status: instance.StatusEnabled, Health: instance.HealthHealthy},
			{ID: fixedID(t, 42), ServiceID: svcID, VersionID: &v2ID, Address: "10.0.0.3", Port: 9103,
				Status: instance.StatusEnabled, Health: instance.HealthHealthy},
			{ID: fixedID(t, 43), ServiceID: svcID, VersionID: &v2ID, Address: "10.0.0.4", Port: 9104,
				Status: instance.StatusEnabled, Health: instance.HealthHealthy},
		},
	}
	groups := map[uuid.UUID][]VersionGroup{
		svcID: {
			{VersionID: v1ID, Version: "v1", Weight: 90, Status: "stable"},
			{VersionID: v2ID, Version: "v2", Weight: 10, Status: "canary"},
		},
	}
	hh := "x-user-id"
	policies := map[uuid.UUID][]traffic.Policy{
		svcID: {{
			ID: vid(t, 9), ServiceID: svcID, Name: "canary-by-header",
			Priority: 1,
			Match:    traffic.Match{Header: map[string]string{"x-canary": "beta"}},
			TargetVersionID: &v2ID, Status: traffic.StatusEnabled,
			Sticky: &traffic.Sticky{HeaderName: hh, CookieName: "MAPLE_SRV"},
		}},
	}
	table := buildTable(tenants, services, domains, insts, groups, policies, nil, nil)
	c := New(tenant.NewRepository(nil), domain.NewRepository(nil), service.NewRepository(nil), instance.NewRepository(nil))
	c.mu.Lock()
	c.table = table
	c.mu.Unlock()
	return c
}

func TestBuildTable_VersionedRouting(t *testing.T) {
	tenID := fixedID(t, 1)
	svcID := fixedID(t, 2)
	domID := fixedID(t, 3)
	v1ID := vid(t, 1)
	v2ID := vid(t, 2)
	inst1 := fixedID(t, 40)
	inst2 := fixedID(t, 41)

	tenants := []*tenant.Tenant{{ID: tenID, Status: tenant.StatusActive}}
	services := []*service.Service{{ID: svcID, TenantID: tenID, Protocol: "http"}}
	domains := []*domain.Domain{
		{ID: domID, TenantID: tenID, Hostname: "ver.example.com", ServiceID: &svcID, Status: domain.StatusActive},
	}
	insts := map[uuid.UUID][]*instance.Instance{
		svcID: {
			{ID: inst1, ServiceID: svcID, VersionID: &v1ID, Address: "10.0.0.1", Port: 9101,
				Protocol: "http", Status: instance.StatusEnabled, Health: instance.HealthHealthy},
			{ID: inst2, ServiceID: svcID, VersionID: &v2ID, Address: "10.0.0.2", Port: 9102,
				Protocol: "http", Status: instance.StatusEnabled, Health: instance.HealthHealthy},
		},
	}
	groups := map[uuid.UUID][]VersionGroup{
		svcID: {
			{DeploymentID: fixedID(t, 5), VersionID: v1ID, Version: "v1", Weight: 90, Status: "stable"},
			{DeploymentID: fixedID(t, 5), VersionID: v2ID, Version: "v2", Weight: 10, Status: "canary"},
		},
	}
	policies := map[uuid.UUID][]traffic.Policy{
		svcID: {{
			ID: vid(t, 9), ServiceID: svcID, Name: "canary-by-header",
			Priority: 1,
			Match:    traffic.Match{Header: map[string]string{"x-canary": "beta"}},
			TargetVersionID: &v2ID, Status: traffic.StatusEnabled,
		}},
	}

	table := buildTable(tenants, services, domains, insts, groups, policies, nil, nil)
	if table == nil {
		t.Fatal("expected table")
	}
	e := table.Lookup("ver.example.com")
	if e == nil {
		t.Fatal("route missing")
	}
	if !e.HasVersions {
		t.Fatal("expected versioned routing")
	}
	if len(e.Versions) != 2 {
		t.Fatalf("versions = %d, want 2", len(e.Versions))
	}
	if len(e.Policies) != 1 {
		t.Fatalf("policies = %d, want 1", len(e.Policies))
	}
}

func TestResolver_VersionWeightedSplit(t *testing.T) {
	c := newTestVersionedCache(t)
	r := NewResolver(c)

	// 90/10 版本分流：长样本接近 9:1。
	hits := map[string]int{}
	for i := 0; i < 2000; i++ {
		target, err := r.Resolve(context.Background(), "ver.example.com")
		if err != nil {
			t.Fatalf("resolve err: %v", err)
		}
		hits[target.Host]++
	}
	if hits["10.0.0.1:9101"] < 1600 || hits["10.0.0.1:9101"] > 2000 {
		t.Fatalf("v1(90) got %d, want ~1800", hits["10.0.0.1:9101"])
	}
	if hits["10.0.0.2:9102"] < 100 || hits["10.0.0.2:9102"] > 400 {
		t.Fatalf("v2(10) got %d, want ~200", hits["10.0.0.2:9102"])
	}
}

func TestResolver_HeaderPolicyRoutesToVersion(t *testing.T) {
	c := newTestVersionedCache(t)
	r := NewResolver(c)

	// header x-canary: beta → 应固定进 v2。
	for i := 0; i < 100; i++ {
		target, err := r.ResolveWith(context.Background(), "ver.example.com", router.MatchView{
			Header: map[string]string{"x-canary": "beta"},
		})
		if err != nil {
			t.Fatalf("resolve err: %v", err)
		}
		if target.Host != "10.0.0.2:9102" {
			t.Fatalf("header-policy target = %s, want v2", target.Host)
		}
	}
	// 无 header → 走权重。
	noHdr := map[string]int{}
	for i := 0; i < 500; i++ {
		target, _ := r.Resolve(context.Background(), "ver.example.com")
		noHdr[target.Host]++
	}
	if noHdr["10.0.0.2:9102"] == 500 {
		t.Fatal("no-header traffic must not all go to v2")
	}
}

// newTestVersionedCache 构建支持版本分流的 cache（两个版本各一个健康实例）。
func newTestVersionedCache(t *testing.T) *Cache {
	t.Helper()
	tenID := fixedID(t, 1)
	svcID := fixedID(t, 2)
	domID := fixedID(t, 3)
	v1ID := vid(t, 1)
	v2ID := vid(t, 2)

	tenants := []*tenant.Tenant{{ID: tenID, Status: tenant.StatusActive}}
	services := []*service.Service{{ID: svcID, TenantID: tenID, Protocol: "http"}}
	domains := []*domain.Domain{
		{ID: domID, TenantID: tenID, Hostname: "ver.example.com", ServiceID: &svcID, Status: domain.StatusActive},
	}
	insts := map[uuid.UUID][]*instance.Instance{
		svcID: {
			{ID: fixedID(t, 40), ServiceID: svcID, VersionID: &v1ID, Address: "10.0.0.1", Port: 9101,
				Protocol: "http", Status: instance.StatusEnabled, Health: instance.HealthHealthy},
			{ID: fixedID(t, 41), ServiceID: svcID, VersionID: &v2ID, Address: "10.0.0.2", Port: 9102,
				Protocol: "http", Status: instance.StatusEnabled, Health: instance.HealthHealthy},
		},
	}
	groups := map[uuid.UUID][]VersionGroup{
		svcID: {
			{VersionID: v1ID, Version: "v1", Weight: 90, Status: "stable"},
			{VersionID: v2ID, Version: "v2", Weight: 10, Status: "canary"},
		},
	}
	policies := map[uuid.UUID][]traffic.Policy{
		svcID: {{
			ID: vid(t, 9), ServiceID: svcID, Name: "canary-by-header",
			Priority: 1,
			Match:    traffic.Match{Header: map[string]string{"x-canary": "beta"}},
			TargetVersionID: &v2ID, Status: traffic.StatusEnabled,
		}},
	}
	table := buildTable(tenants, services, domains, insts, groups, policies, nil, nil)
	c := New(tenant.NewRepository(nil), domain.NewRepository(nil), service.NewRepository(nil), instance.NewRepository(nil))
	c.mu.Lock()
	c.table = table
	c.mu.Unlock()
	return c
}

func TestResolver_ZeroWeightVersionGetsNoTraffic(t *testing.T) {
	// canary 权重归 0（如 rollback 后 standby）→ 该版本完全无流量，全部落到 stable。
	c := newTestVersionedCache(t)
	// 直接把 v2 权重改为 0（模拟 rollback 后的 deployment_versions.weight=0）。
	// v2 仍保持 canary 状态与健康实例（仅 weight=0），验证 pickVersionIndex 过滤。
	c.mu.Lock()
	e := c.table.Lookup("ver.example.com")
	for i := range e.Versions {
		if e.Versions[i].Version == "v2" {
			e.Versions[i].Weight = 0
		}
	}
	c.mu.Unlock()

	r := NewResolver(c)
	hits := map[string]int{}
	for i := 0; i < 500; i++ {
		target, err := r.Resolve(context.Background(), "ver.example.com")
		if err != nil {
			t.Fatalf("resolve err: %v", err)
		}
		hits[target.Host]++
	}
	if hits["10.0.0.2:9102"] != 0 {
		t.Fatalf("zero-weight canary got %d requests, want 0", hits["10.0.0.2:9102"])
	}
	if hits["10.0.0.1:9101"] != 500 {
		t.Fatalf("stable got %d, want all 500", hits["10.0.0.1:9101"])
	}
}
