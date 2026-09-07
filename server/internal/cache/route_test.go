package cache

import (
	"testing"

	"github.com/NeoPlayful/maple-gateway/server/internal/domain"
	"github.com/NeoPlayful/maple-gateway/server/internal/instance"
	"github.com/NeoPlayful/maple-gateway/server/internal/service"
	"github.com/NeoPlayful/maple-gateway/server/internal/tenant"
	"github.com/google/uuid"
)

func fixedID(t *testing.T, n byte) uuid.UUID {
	t.Helper()
	id := uuid.Nil
	id[0] = n
	return id
}

func TestBuildTable_BasicRoute(t *testing.T) {
	tenID := fixedID(t, 1)
	svcID := fixedID(t, 2)
	domID := fixedID(t, 3)
	instID := fixedID(t, 4)

	tenants := []*tenant.Tenant{{ID: tenID, Status: tenant.StatusActive}}
	services := []*service.Service{{ID: svcID, TenantID: tenID, Protocol: "http"}}
	domains := []*domain.Domain{
		{ID: domID, TenantID: tenID, Hostname: "shop-a.example.com", ServiceID: &svcID, Status: domain.StatusActive},
	}
	insts := map[uuid.UUID][]*instance.Instance{
		svcID: {
			{ID: instID, ServiceID: svcID, Address: "10.0.0.5", Port: 9101,
				Protocol: "http", Status: instance.StatusEnabled, Health: instance.HealthHealthy},
		},
	}

	table := buildTable(tenants, services, domains, insts)
	if table == nil {
		t.Fatal("expected non-nil table")
	}
	e := table.Lookup("shop-a.example.com")
	if e == nil {
		t.Fatal("route for host not found")
	}
	if !e.TenantOK {
		t.Fatal("tenant should be OK")
	}
	if len(e.Pool) != 1 {
		t.Fatalf("pool len = %d, want 1", len(e.Pool))
	}
	if e.Pool[0].Endpoint != "10.0.0.5:9101" {
		t.Fatalf("endpoint = %q", e.Pool[0].Endpoint)
	}
}

func TestBuildTable_Filters(t *testing.T) {
	tenID := fixedID(t, 1)
	svcID := fixedID(t, 2)
	instUnhealthy := fixedID(t, 5)
	instDraining := fixedID(t, 6)

	activeTenant := &tenant.Tenant{ID: tenID, Status: tenant.StatusActive}
	suspendedTenant := &tenant.Tenant{ID: fixedID(t, 7), Status: tenant.StatusSuspended}
	svc2ID := fixedID(t, 8)
	domDisabled := fixedID(t, 9)
	domNoService := fixedID(t, 10)

	// service2 挂到 suspended tenant 下。
	services := []*service.Service{
		{ID: svcID, TenantID: tenID, Protocol: "http"},
		{ID: svc2ID, TenantID: suspendedTenant.ID, Protocol: "http"},
	}
	domains := []*domain.Domain{
		// disabled domain 应被过滤
		{ID: domDisabled, TenantID: tenID, Hostname: "disabled.example.com",
			ServiceID: &svcID, Status: domain.StatusDisabled},
		// 无默认 service 应被过滤
		{ID: domNoService, TenantID: tenID, Hostname: "nosvc.example.com", Status: domain.StatusActive},
		// 正常，但池中只有不健康/排空实例
		{ID: fixedID(t, 11), TenantID: tenID, Hostname: "emptypool.example.com",
			ServiceID: &svcID, Status: domain.StatusActive},
		// suspended tenant 的域名
		{ID: fixedID(t, 12), TenantID: suspendedTenant.ID, Hostname: "susp.example.com",
			ServiceID: &svc2ID, Status: domain.StatusActive},
	}
	insts := map[uuid.UUID][]*instance.Instance{
		svcID: {
			{ID: instUnhealthy, ServiceID: svcID, Address: "1.1.1.1", Port: 80,
				Status: instance.StatusEnabled, Health: instance.HealthUnhealthy},
			{ID: instDraining, ServiceID: svcID, Address: "1.1.1.2", Port: 80,
				Status: instance.StatusDraining, Health: instance.HealthHealthy},
		},
	}

	table := buildTable([]*tenant.Tenant{activeTenant, suspendedTenant}, services, domains, insts)
	if table == nil {
		t.Fatal("expected table (some routes present)")
	}
	if table.Lookup("disabled.example.com") != nil {
		t.Fatal("disabled domain should be filtered")
	}
	if table.Lookup("nosvc.example.com") != nil {
		t.Fatal("domain without default service should be filtered")
	}

	empty := table.Lookup("emptypool.example.com")
	if empty == nil {
		t.Fatal("emptypool domain should have an entry (tenant+service ok)")
	}
	if len(empty.Pool) != 0 {
		t.Fatalf("pool should be empty, got %d", len(empty.Pool))
	}

	susp := table.Lookup("susp.example.com")
	if susp == nil {
		t.Fatal("suspended tenant route should exist as entry")
	}
	if susp.TenantOK {
		t.Fatal("suspended tenant should be marked not OK")
	}
}

func TestBuildTable_NormalizeHost(t *testing.T) {
	tenID := fixedID(t, 1)
	svcID := fixedID(t, 2)
	domains := []*domain.Domain{
		{ID: fixedID(t, 3), TenantID: tenID, Hostname: "Mixed.Case.Example.com",
			ServiceID: &svcID, Status: domain.StatusActive},
	}
	table := buildTable(
		[]*tenant.Tenant{{ID: tenID, Status: tenant.StatusActive}},
		[]*service.Service{{ID: svcID, TenantID: tenID, Protocol: "http"}},
		domains, nil,
	)
	if table.Lookup("MIXED.case.example.com") == nil {
		t.Fatal("lookup should be case-insensitive")
	}
	if table.Lookup("mixed.case.example.com:8080") == nil {
		t.Fatal("lookup should ignore trailing port")
	}
}
