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
	"github.com/NeoPlayful/maple-gateway/server/internal/ratelimit"
	"github.com/NeoPlayful/maple-gateway/server/internal/router"
	"github.com/NeoPlayful/maple-gateway/server/internal/service"
	"github.com/NeoPlayful/maple-gateway/server/internal/tenant"
	"github.com/NeoPlayful/maple-gateway/server/internal/traffic"
	"github.com/google/uuid"
)

// VersionPool 是单个版本下的健康实例池。
type VersionPool struct {
	VersionID uuid.UUID
	Version   string
	Weight    int // 版本权重（WRR），0 表示不参与分配
	Pool      []router.PoolMember
}

// RouteEntry 描述一个 Host 命中的完整路由信息。
// 无版本体系时用 Pool（Phase 1 直挂）；挂 deployment/version 时用 Versions 并按 Policies 分流。
type RouteEntry struct {
	DomainID  uuid.UUID
	Hostname  string
	TenantID  uuid.UUID
	ServiceID uuid.UUID
	Protocol  string
	// TenantOK 指示租户状态可路由（active）。false 时数据平面应拒绝。
	TenantOK bool
	Pool     []router.PoolMember // 仅 healthy + enabled 实例
	// HasVersions 指示该路由走版本分流（true 时 Versions/Policies 有效，Pool 保持为空）。
	HasVersions bool
	Versions    []VersionPool
	Policies    []traffic.Policy
	// Limits 是此路由适用的限流规则（global + tenant + domain + service 维度）。
	Limits []ratelimit.RateLimit
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

// VersionSource 抽象部署版本与策略的加载，供路由表携带版本/策略。
// 由装配方用 deployment + traffic repository 实现；为 nil 时路由退化为 Phase 1 直挂模式。
type VersionSource interface {
	// LoadVersioning 返回 service_id → 版本池 与 service_id → 策略。
	// 返回的映射可能为空（无版本体系的服务）。
	LoadVersioning(ctx context.Context) (map[uuid.UUID][]VersionGroup,
		map[uuid.UUID][]traffic.Policy, error)
}

// VersionGroup 是路由表构建用的临时聚合：一个部署版本及其权重（实例由 cache 侧关联）。
type VersionGroup struct {
	DeploymentID uuid.UUID
	VersionID    uuid.UUID
	Version      string
	Weight       int
	Status       string // stable / canary / active / standby / draining / inactive
}

// Cache 持有当前 RouteTable，并发安全，原子替换。
type Cache struct {
	mu    sync.RWMutex
	table *RouteTable

	tenants   *tenant.Repository
	domains   *domain.Repository
	services  *service.Repository
	instances *instance.Repository
	versions  VersionSource // 可选；nil 时无版本分流
	loadNodes func(ctx context.Context) (map[uuid.UUID]bool, error) // 可选；nil 时不过滤 offline 节点
	loadLimits func(ctx context.Context) ([]ratelimit.RateLimit, error) // 可选；nil 时无速率限制

	// 统计与可用性。
	hits   atomic.Uint64
	misses atomic.Uint64
}

// New 构造 Cache（Phase 1 语义，不含版本分流）。repos 为 nil 时 Rebuild 将返回错误。
func New(tr *tenant.Repository, dr *domain.Repository, sr *service.Repository, ir *instance.Repository) *Cache {
	return newCache(tr, dr, sr, ir, nil)
}

// NewVersioned 构造支持版本分流的 Cache。versions 为 nil 时等价于 New。
func NewVersioned(tr *tenant.Repository, dr *domain.Repository, sr *service.Repository,
	ir *instance.Repository, versions VersionSource) *Cache {
	return newCache(tr, dr, sr, ir, versions)
}

func newCache(tr *tenant.Repository, dr *domain.Repository, sr *service.Repository,
	ir *instance.Repository, versions VersionSource) *Cache {
	return &Cache{
		tenants:   tr,
		domains:   dr,
		services:  sr,
		instances: ir,
		versions:  versions,
	}
}

// WithNodeFilter 注入节点可路由映射加载器。loader 为 nil 时取消节点过滤。
// node 非 online（offline/maintenance/disabled）时其上实例不参与路由。
func (c *Cache) WithNodeFilter(loader func(ctx context.Context) (map[uuid.UUID]bool, error)) *Cache {
	c.loadNodes = loader
	return c
}

// WithLimits 注入限流规则加载器。loader 为 nil 时取消限流。
func (c *Cache) WithLimits(loader func(ctx context.Context) ([]ratelimit.RateLimit, error)) *Cache {
	c.loadLimits = loader
	return c
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

	var groups map[uuid.UUID][]VersionGroup
	var policies map[uuid.UUID][]traffic.Policy
	if c.versions != nil {
		groups, policies, err = c.versions.LoadVersioning(ctx)
		if err != nil {
			return fmt.Errorf("route cache rebuild versioning: %w", err)
		}
	}

	var nodeRoutable map[uuid.UUID]bool
	if c.loadNodes != nil {
		nodeRoutable, err = c.loadNodes(ctx)
		if err != nil {
			return fmt.Errorf("route cache rebuild nodes: %w", err)
		}
	}

	var limits []ratelimit.RateLimit
	if c.loadLimits != nil {
		limits, err = c.loadLimits(ctx)
		if err != nil {
			return fmt.Errorf("route cache rebuild limits: %w", err)
		}
	}

	table := buildTable(tenants, services, domains, insts, groups, policies, nodeRoutable, limits)
	c.mu.Lock()
	c.table = table
	c.mu.Unlock()
	return nil
}

// buildTable 是纯函数：把 DB 数据组装为不可变路由表。
// groups/policies 可为 nil（Phase 1 无版本分流）；nodeRoutable 为 nil 时不按节点过滤；
// limits 可为 nil（无速率限制）。返回 nil 表表示"无可路由内容"。
func buildTable(tenants []*tenant.Tenant, services []*service.Service,
	domains []*domain.Domain, insts map[uuid.UUID][]*instance.Instance,
	groups map[uuid.UUID][]VersionGroup, policies map[uuid.UUID][]traffic.Policy,
	nodeRoutable map[uuid.UUID]bool, limits []ratelimit.RateLimit) *RouteTable {

	tenantStatus := make(map[uuid.UUID]bool, len(tenants))
	for _, t := range tenants {
		tenantStatus[t.ID] = t.Status == tenant.StatusActive
	}
	// nodeOK 判断实例所在节点是否可路由（无 node_id 视为可路由；nodeRoutable 为空视为不限制）。
	nodeOK := func(nodeID *uuid.UUID) bool {
		if nodeID == nil {
			return true
		}
		if len(nodeRoutable) == 0 {
			return true
		}
		return nodeRoutable[*nodeID]
	}
	routable := func(in *instance.Instance) bool {
		return in.IsRoutable() && nodeOK(in.NodeID)
	}
	serviceProto := make(map[uuid.UUID]string, len(services))
	serviceTenant := make(map[uuid.UUID]uuid.UUID, len(services))
	for _, s := range services {
		serviceProto[s.ID] = s.Protocol
		serviceTenant[s.ID] = s.TenantID
	}

	// limitsFor 按 domain/tenant/service 归属预分组，避免每 entry 全量扫描。
	domainLim := map[uuid.UUID][]ratelimit.RateLimit{}
	tenantLim := map[uuid.UUID][]ratelimit.RateLimit{}
	svcLim := map[uuid.UUID][]ratelimit.RateLimit{}
	var globalLim []ratelimit.RateLimit
	for _, l := range limits {
		switch l.Scope {
		case ratelimit.ScopeGlobal, ratelimit.ScopeIP:
			// ip scope 规则作用于每个 entry（判定时按客户端 IP 独立计数）。
			globalLim = append(globalLim, l)
		case ratelimit.ScopeDomain:
			if l.DomainID != nil {
				domainLim[*l.DomainID] = append(domainLim[*l.DomainID], l)
			}
		case ratelimit.ScopeTenant:
			if l.TenantID != nil {
				tenantLim[*l.TenantID] = append(tenantLim[*l.TenantID], l)
			}
		case ratelimit.ScopeService:
			if l.ServiceID != nil {
				svcLim[*l.ServiceID] = append(svcLim[*l.ServiceID], l)
			}
		}
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
		// 路由适用限流 = global + domain + tenant + service 维度规则（判定时取最严）。
		entry.Limits = append(entry.Limits, globalLim...)
		entry.Limits = append(entry.Limits, domainLim[d.ID]...)
		entry.Limits = append(entry.Limits, tenantLim[tenID]...)
		entry.Limits = append(entry.Limits, svcLim[sid]...)

		// 版本分流：service 声明了版本组且实例归属版本时使用。
		if gs := groups[sid]; len(gs) > 0 {
			entry.HasVersions = true
			// 先收集每个版本挂到的健康实例。
			instByVer := map[uuid.UUID][]*instance.Instance{}
			hasVersionedInst := false
			for _, in := range insts[sid] {
				if !routable(in) {
					continue
				}
				if in.VersionID != nil {
					hasVersionedInst = true
					instByVer[*in.VersionID] = append(instByVer[*in.VersionID], in)
				}
			}
			if hasVersionedInst {
				for _, g := range gs {
					if !versionRoutable(g.Status) {
						continue
					}
					pool := make([]router.PoolMember, 0, len(instByVer[g.VersionID]))
					for _, in := range instByVer[g.VersionID] {
						pool = append(pool, router.PoolMember{
							ID:       in.ID.String(),
							Endpoint: in.Endpoint(),
							Protocol: in.Protocol,
							Weight:   in.Weight,
						})
					}
					if len(pool) == 0 {
						continue // 该版本无健康实例：不进池（权重不参与分配）
					}
					entry.Versions = append(entry.Versions, VersionPool{
						VersionID: g.VersionID,
						Version:   g.Version,
						Weight:    g.Weight,
						Pool:      pool,
					})
				}
				entry.Policies = policies[sid]
			}
			// 若没有任何版本有健康实例，HasVersions 保持 true 且 Versions 空 → resolver 返回 no healthy。
			if len(entry.Versions) == 0 {
				byHost[normalizeHost(d.Hostname)] = entry
				continue
			}
			byHost[normalizeHost(d.Hostname)] = entry
			continue
		}

		// Phase 1 直挂池。
		for _, in := range insts[sid] {
			if !routable(in) {
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

// versionRoutable 判断版本状态是否参与流量分配。
func versionRoutable(status string) bool {
	switch status {
	case "stable", "canary", "active", "standby":
		return true
	default: // draining / inactive
		return false
	}
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
