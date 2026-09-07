package cache

import (
	"context"
	"hash/fnv"
	"strings"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/internal/loadbalancer"
	"github.com/NeoPlayful/maple-gateway/server/internal/ratelimit"
	"github.com/NeoPlayful/maple-gateway/server/internal/router"
	"github.com/NeoPlayful/maple-gateway/server/internal/traffic"
	"github.com/google/uuid"
)

// CacheResolver 把路由缓存接入 router.Resolver。
//
// 每个 Host 命中后，先按路由限流规则判定（任一 scope 超限 → ErrRateLimited）；
// 再在版本分流（HasVersions）时按策略/版本权重选择版本，在该版本健康实例池中
// 用 Load Balancer 选择目标；否则退化为 Phase 1 直挂池。
type CacheResolver struct {
	cache    *Cache
	balancer *loadbalancer.Balancer
	limiter  *ratelimit.Limiter
}

// NewResolver 构造。
func NewResolver(c *Cache) *CacheResolver {
	return &CacheResolver{
		cache:    c,
		balancer: loadbalancer.NewBalancer(),
		limiter:  ratelimit.NewLimiter(0),
	}
}

// Resolve 实现 router.Resolver：Host → 目标实例。
// 无请求上下文（无法匹配 header/path）时，版本分流仅按版本权重进行。
func (r *CacheResolver) Resolve(ctx context.Context, host string) (*router.Target, error) {
	return r.resolveWith(host, traffic.RequestView{})
}

// ResolveWith 实现 router.ContextResolver：携带请求匹配上下文（header/path）分流。
func (r *CacheResolver) ResolveWith(ctx context.Context, host string, mv router.MatchView) (*router.Target, error) {
	return r.resolveWith(host, traffic.ViewFrom(mv))
}

func (r *CacheResolver) resolveWith(host string, rv traffic.RequestView) (*router.Target, error) {
	e := r.cache.Lookup(host)
	if e == nil {
		return nil, router.ErrNotFound
	}
	if !e.TenantOK {
		return nil, router.ErrTenantSuspended
	}
	if len(e.Limits) > 0 {
		if err := r.enforceLimits(e, rv); err != nil {
			return nil, err
		}
	}

	if e.HasVersions {
		return r.selectVersioned(e, rv)
	}
	m := r.balancer.Select(e.DomainID.String(), e.Pool)
	if m == nil {
		return nil, router.ErrNoHealthy
	}
	return targetFromMember(e, m), nil
}

// selectVersioned 版本分流：策略命中优先（含 sticky 会话保持），否则按版本权重 WRR。
func (r *CacheResolver) selectVersioned(e *RouteEntry, rv traffic.RequestView) (*router.Target, error) {
	if len(e.Versions) == 0 {
		return nil, router.ErrNoHealthy
	}

	// 1. 按 priority 找到命中策略（Header/Cookie/Path 条件 AND）。
	if p := matchPolicy(e, rv); p != nil {
		if p.TargetVersionID == nil {
			// 策略无定向版本：退化为版本权重（如 sticky-only 策略）。
			return r.pickWeightedVersion(e)
		}
		// sticky 策略：用会话键做一致性哈希，保证同会话恒同实例。
		if p.Sticky != nil {
			if key := stickyKey(p.Sticky, rv); key != "" {
				return r.pickFromVersionSticky(e, *p.TargetVersionID, key)
			}
		}
		return r.pickFromVersion(e, *p.TargetVersionID)
	}

	// 2. 无策略命中：按版本权重选版本，再实例 LB。
	return r.pickWeightedVersion(e)
}

// enforceLimits 逐条判定路由限流规则；任一规则超限返回 ErrRateLimited。
// key 按 scope 区分：global 用 host 池；domain/tenant/service 用对应 ID；ip 用客户端 IP。
func (r *CacheResolver) enforceLimits(e *RouteEntry, rv traffic.RequestView) error {
	now := time.Now()
	for i := range e.Limits {
		l := &e.Limits[i]
		key := r.limitKey(e, l, rv)
		burst := l.Burst
		if burst <= 0 {
			burst = l.Limit
		}
		if a := r.limiter.Allow(key, l.Limit, l.WindowSeconds, burst, now); !a.Allowed {
			return router.ErrRateLimited
		}
	}
	return nil
}

func (r *CacheResolver) limitKey(e *RouteEntry, l *ratelimit.RateLimit, rv traffic.RequestView) string {
	switch l.Scope {
	case ratelimit.ScopeDomain:
		return "domain:" + e.DomainID.String()
	case ratelimit.ScopeTenant:
		return "tenant:" + e.TenantID.String()
	case ratelimit.ScopeService:
		return "service:" + e.ServiceID.String()
	case ratelimit.ScopeIP:
		ip := rv.ClientIP
		if ip == "" {
			ip = "unknown"
		}
		return "ip:" + ip
	default:
		return "global:" + e.DomainID.String() // global 按域名独立计数
	}
}

// stickyKey 从请求提取会话键：优先指定 header，其次指定 cookie。
func stickyKey(s *traffic.Sticky, rv traffic.RequestView) string {
	if s.HeaderName != "" {
		if v := rv.Header.Get(s.HeaderName); v != "" {
			return v
		}
	}
	name := s.CookieName
	if name == "" {
		name = "MAPLE_SRV"
	}
	if raw := rv.Header.Get("Cookie"); raw != "" {
		if v := cookieValue(raw, name); v != "" {
			return v
		}
	}
	return ""
}

// pickFromVersionSticky 在目标版本池内用会话键一致性哈希选实例。
func (r *CacheResolver) pickFromVersionSticky(e *RouteEntry, versionID uuid.UUID, key string) (*router.Target, error) {
	var pool []router.PoolMember
	for _, v := range e.Versions {
		if v.VersionID == versionID {
			pool = v.Pool
			break
		}
	}
	if len(pool) == 0 {
		return r.pickFallback(e, versionID)
	}
	m := stickySelect(pool, key)
	if m == nil {
		return nil, router.ErrNoHealthy
	}
	return targetFromMember(e, m), nil
}

// pickWeightedVersion 版本权重层 WRR → 该版本实例池 WRR。
// weight<=0 的版本视为不参与分配（canary 权重归 0 时完全无流量）。
func (r *CacheResolver) pickWeightedVersion(e *RouteEntry) (*router.Target, error) {
	vi := r.pickVersionIndex(e.DomainID.String()+"#versions", e.Versions, func(_ VersionPool) bool { return true })
	if vi < 0 {
		return nil, router.ErrNoHealthy
	}
	return r.pickFromVersion(e, e.Versions[vi].VersionID)
}

// pickFromVersion 从指定版本的健康实例池中选一个实例。
// 该版本无健康实例时回退到其余版本按权重分配（版本自身健康摘除语义）。
func (r *CacheResolver) pickFromVersion(e *RouteEntry, versionID uuid.UUID) (*router.Target, error) {
	var pool []router.PoolMember
	for _, v := range e.Versions {
		if v.VersionID == versionID {
			pool = v.Pool
			break
		}
	}
	if len(pool) == 0 {
		return r.pickFallback(e, versionID)
	}
	m := r.balancer.Select(e.DomainID.String()+"#inst:"+versionID.String(), pool)
	if m == nil {
		return nil, router.ErrNoHealthy
	}
	return targetFromMember(e, m), nil
}

// pickVersionIndex 在候选版本池中按平滑加权选一个下标；weight<=0 的版本不参与分配。
// allow 过滤候选（如 fallback 排除指定版本与无健康实例版本）。无可选返回 -1。
func (r *CacheResolver) pickVersionIndex(key string, versions []VersionPool, allow func(VersionPool) bool) int {
	pool := make([]VersionPool, 0, len(versions))
	for _, v := range versions {
		if v.Weight <= 0 || !allow(v) {
			continue
		}
		pool = append(pool, v)
	}
	if len(pool) == 0 {
		return -1
	}
	weights := make([]int, len(pool))
	for i, v := range pool {
		weights[i] = v.Weight
	}
	vi := r.balancer.PickWeighted(key, weights)
	if vi < 0 {
		return -1
	}
	for i, v := range versions {
		if v.VersionID == pool[vi].VersionID {
			return i
		}
	}
	return -1
}

// pickFallback 目标版本无健康实例：在其余可路由版本中按权重再选一次。
func (r *CacheResolver) pickFallback(e *RouteEntry, skip uuid.UUID) (*router.Target, error) {
	vi := r.pickVersionIndex(e.DomainID.String()+"#versions-fb", e.Versions, func(v VersionPool) bool {
		return v.VersionID != skip && len(v.Pool) > 0
	})
	if vi < 0 {
		return nil, router.ErrNoHealthy
	}
	return r.pickFromVersion(e, e.Versions[vi].VersionID)
}

// matchPolicy 在 e.Policies 中按 priority 升序找首个命中策略；无命中返回 nil。
// 匹配仅判定条件（Header/Cookie/Path），不含 sticky 副作用（S3 处理）。
func matchPolicy(e *RouteEntry, rv traffic.RequestView) *traffic.Policy {
	var best *traffic.Policy
	for i := range e.Policies {
		p := &e.Policies[i]
		if !p.Matches(rv) {
			continue
		}
		if best == nil || p.Priority < best.Priority {
			best = p
		}
	}
	return best
}

func targetFromMember(e *RouteEntry, m *router.PoolMember) *router.Target {
	scheme := m.Protocol
	if scheme == "" {
		scheme = e.Protocol
	}
	if scheme == "" {
		scheme = "http"
	}
	return &router.Target{Scheme: scheme, Host: m.Endpoint}
}

// stickySelect 用会话键对池做一致性哈希选择（同 key 恒同实例，pool 变化时重映射）。
func stickySelect(pool []router.PoolMember, key string) *router.PoolMember {
	if len(pool) == 0 {
		return nil
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	idx := int(h.Sum32() % uint32(len(pool)))
	return &pool[idx]
}

// cookieValue 解析单个 cookie 值（CacheResolver 内部副本，避免跨包依赖私有实现）。
func cookieValue(raw, name string) string {
	for _, part := range strings.Split(raw, ";") {
		part = strings.TrimSpace(part)
		if kv := strings.SplitN(part, "=", 2); len(kv) == 2 && kv[0] == name {
			return kv[1]
		}
	}
	return ""
}
