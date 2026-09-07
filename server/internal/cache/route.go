// Package cache 维护数据平面的内存路由表。
// 请求主链路只读此表，不查询 PostgreSQL；配置变更通过 Rebuild 同步。
package cache

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/NeoPlayful/maple-gateway/server/internal/domain"
	"github.com/NeoPlayful/maple-gateway/server/internal/instance"
	"github.com/NeoPlayful/maple-gateway/server/internal/router"
	"github.com/NeoPlayful/maple-gateway/server/internal/service"
	"github.com/NeoPlayful/maple-gateway/server/internal/tenant"
	"github.com/google/uuid"
)

// RouteEntry 描述一个 Host 命中的完整路由信息。
type RouteEntry struct {
	DomainID  uuid.UUID
	Hostname  string
	TenantID  uuid.UUID
	ServiceID uuid.UUID
	Protocol  string
	// TenantOK 指示租户状态可路由（active）。false 时数据平面应拒绝。
	TenantOK bool
	Pool     []router.PoolMember // 仅 healthy + enabled 实例
}

// RouteTable 是不可变快照，整体替换保证读一致性。
type RouteTable struct {
	byHost map[string]*RouteEntry
}

// Lookup 按 hostname 查路由（内部会规范化大小写与端口），找不到返回 nil。
func (t *RouteTable) Lookup(host string) *RouteEntry {
	if t == nil {
		return nil
	}
	return t.byHost[normalizeHost(host)]
}

// Entries 返回全部路由项（管理端诊断）。
func (t *RouteTable) Entries() []*RouteEntry {
	if t == nil {
		return nil
	}
	out := make([]*RouteEntry, 0, len(t.byHost))
	for _, e := range t.byHost {
		out = append(out, e)
	}
	return out
}

// Cache 持有当前 RouteTable，并发安全，原子替换。
type Cache struct {
	mu    sync.RWMutex
	table *RouteTable

	tenants   *tenant.Repository
	domains   *domain.Repository
	services  *service.Repository
	instances *instance.Repository

	// 统计与可用性。
	hits   atomic.Uint64
	misses atomic.Uint64
}

// New 构造 Cache。repos 为 nil 时 Rebuild 将返回错误（保持 last-good 语义）。
func New(tr *tenant.Repository, dr *domain.Repository, sr *service.Repository, ir *instance.Repository) *Cache {
	return &Cache{
		tenants:   tr,
		domains:   dr,
		services:  sr,
		instances: ir,
	}
}

// Lookup 命中 +1。host 规范化由 RouteTable.Lookup 内部完成。
func (c *Cache) Lookup(host string) *RouteEntry {
	c.mu.RLock()
	e := c.table.Lookup(host)
	c.mu.RUnlock()
	if e != nil {
		c.hits.Add(1)
	} else {
		c.misses.Add(1)
	}
	return e
}

// Stats 返回统计。
func (c *Cache) Stats() (hits, misses uint64) {
	return c.hits.Load(), c.misses.Load()
}

// Entries 返回当前全部路由项（管理端诊断）。
func (c *Cache) Entries() []*RouteEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.table.Entries()
}

// Rebuild 从数据库全量重建路由表并原子替换。
// DB 异常时保留最后有效表并返回错误。
func (c *Cache) Rebuild(ctx context.Context) error {
	if c.tenants == nil || c.domains == nil || c.services == nil || c.instances == nil {
		return fmt.Errorf("route cache: repositories not wired")
	}

	// 全量拉取（分批处理 domains 以防海量数据）。
	tenants, err := c.tenants.All(ctx)
	if err != nil {
		return fmt.Errorf("route cache rebuild tenants: %w", err)
	}
	services, err := c.services.All(ctx)
	if err != nil {
		return fmt.Errorf("route cache rebuild services: %w", err)
	}
	domains, err := c.domains.All(ctx)
	if err != nil {
		return fmt.Errorf("route cache rebuild domains: %w", err)
	}
	insts, err := c.instances.AllGroupedByService(ctx)
	if err != nil {
		return fmt.Errorf("route cache rebuild instances: %w", err)
	}

	table := buildTable(tenants, services, domains, insts)
	c.mu.Lock()
	c.table = table
	c.mu.Unlock()
	return nil
}

// buildTable 是纯函数：把 DB 数据组装为不可变路由表。
// 返回 nil 表表示"无可路由内容"（正常，等配置产生）。
func buildTable(tenants []*tenant.Tenant, services []*service.Service,
	domains []*domain.Domain, insts map[uuid.UUID][]*instance.Instance) *RouteTable {

	tenantStatus := make(map[uuid.UUID]bool, len(tenants))
	for _, t := range tenants {
		tenantStatus[t.ID] = t.Status == tenant.StatusActive
	}
	serviceProto := make(map[uuid.UUID]string, len(services))
	serviceTenant := make(map[uuid.UUID]uuid.UUID, len(services))
	for _, s := range services {
		serviceProto[s.ID] = s.Protocol
		serviceTenant[s.ID] = s.TenantID
	}

	byHost := make(map[string]*RouteEntry, len(domains))
	for _, d := range domains {
		if d.Status != domain.StatusActive {
			continue // disabled / pending 域名不入路由表
		}
		if d.ServiceID == nil {
			continue // 未指定默认 Service，暂不可路由（Phase 1 简化）
		}
		sid := *d.ServiceID
		sproto := serviceProto[sid]
		if sproto == "" {
			continue
		}
		tenID := serviceTenant[sid]

		entry := &RouteEntry{
			DomainID:  d.ID,
			Hostname:  d.Hostname,
			TenantID:  tenID,
			ServiceID: sid,
			Protocol:  sproto,
			TenantOK:  tenantStatus[tenID],
		}
		for _, in := range insts[sid] {
			if !in.IsRoutable() {
				continue
			}
			entry.Pool = append(entry.Pool, router.PoolMember{
				ID:       in.ID.String(),
				Endpoint: in.Endpoint(),
				Protocol: in.Protocol,
				Weight:   in.Weight,
			})
		}
		byHost[normalizeHost(d.Hostname)] = entry
	}

	if len(byHost) == 0 {
		return nil
	}
	return &RouteTable{byHost: byHost}
}

// normalizeHost 缓存 key 规范化，与数据平面使用同一算法（router.NormalizeHost）。
// 畸形 host 规范化失败时返回空串（导致查不到路由，语义安全）。
func normalizeHost(host string) string {
	n, err := router.NormalizeHost(host)
	if err != nil {
		return ""
	}
	return n
}
