package cache

import (
	"context"
	"hash/fnv"
	"strings"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/internal/loadbalancer"
	"github.com/NeoPlayful/maple-gateway/server/internal/metrics"
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
	limiter  ratelimit.Backend // 单机内存 Limiter（默认）或 RedisLimiter（多实例共享）
	metrics  *metrics.Registry // 可选；nil 时不采集限流命中指标
}

// NewResolver 构造。
func NewResolver(c *Cache) *CacheResolver {
	return &CacheResolver{
		cache:    c,
		balancer: loadbalancer.NewBalancer(),
		limiter:  ratelimit.NewLimiter(0),
	}
}

// WithMetrics 注入指标注册表（可选）。nil 时不采集。
func (r *CacheResolver) WithMetrics(m *metrics.Registry) *CacheResolver {
	r.metrics = m
	return r
}

// WithLimiter 注入限流后端（可选）。nil 时用默认单机内存 Limiter。
// 多实例共享配额时可注入基于 Redis 的 RedisLimiter（跨实例计数）。
func (r *CacheResolver) WithLimiter(l ratelimit.Backend) *CacheResolver {
	if l != nil {
		r.limiter = l
	}
	return r
}

// IncConn / DecConn 实现 router.ConnReporter：把数据平面请求的在途连接
// 计入 balancer 的 Least-Connections 计数（按选中实例 ID）。
func (r *CacheResolver) IncConn(instanceID string) { r.balancer.IncConn(instanceID) }
func (r *CacheResolver) DecConn(instanceID string) { r.balancer.DecConn(instanceID) }

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
	return targetFromMember(e, m, uuid.Nil), nil
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
		// sticky 策略：优先按会话键落到同实例，无会话键时选实例并下发 Set-Cookie。
		if p.Sticky != nil {
			return r.pickSticky(e, p, *p.TargetVersionID, rv)
		}
		return r.pickFromVersionBalanced(e, *p.TargetVersionID, p.Balance, rv)
	}

	// 2. 无显式条件命中：存在 percent-only 策略时按比例抽签定向其版本。
	if t, ok, err := r.pickPercentPolicy(e, rv); ok || err != nil {
		return t, err
	}

	// 3. 无策略命中：按版本权重选版本，再实例 LB。
	return r.pickWeightedVersion(e)
}

// pickPercentPolicy 处理只带 percent（无 header/cookie/path 条件）的策略：
// 用稳定会话键（优先 header/cookie，兜底客户端 IP）做确定性抽签，
// 哈希值落入 [0, percent) 则定向该策略版本；否则回落到版本权重。
// 同一会话在窗口内请求应保持同一定向，故同键结果稳定可复现。
func (r *CacheResolver) pickPercentPolicy(e *RouteEntry, rv traffic.RequestView) (*router.Target, bool, error) {
	var chosen *traffic.Policy
	for i := range e.Policies {
		p := &e.Policies[i]
		if p.Status != traffic.StatusEnabled || p.TargetVersionID == nil {
			continue
		}
		m := p.Match
		// 仅当策略无任何显式条件、仅设 percent 时按比例处理。
		if len(m.Header) > 0 || len(m.Cookie) > 0 || m.Path != "" || m.PathExact != "" || m.Percent <= 0 {
			continue
		}
		if chosen == nil || p.Priority < chosen.Priority {
			chosen = p
		}
	}
	if chosen == nil {
		return nil, false, nil
	}

	key := r.stableSessionKey(e, rv)
	if key == "" {
		return nil, false, nil
	}
	bucket := hashBucket(key)
	if bucket < chosen.Match.Percent {
		t, err := r.pickFromVersionBalanced(e, *chosen.TargetVersionID, chosen.Balance, rv)
		return t, err == nil, err
	}
	return nil, false, nil
}

// stableSessionKey 构造 percent 抽签的稳定键：优先 sticky header，其次 cookie，兜底客户端 IP。
func (r *CacheResolver) stableSessionKey(e *RouteEntry, rv traffic.RequestView) string {
	// 任一命中策略若声明 sticky header，优先用它保证会话一致性。
	for i := range e.Policies {
		p := &e.Policies[i]
		if p.Status != traffic.StatusEnabled || p.Sticky == nil || p.Sticky.HeaderName == "" {
			continue
		}
		if v := rv.Header.Get(p.Sticky.HeaderName); v != "" {
			return "h:" + v
		}
	}
	// percent 目标版本存在 sticky cookie 时按 cookie 分桶。
	for i := range e.Policies {
		p := &e.Policies[i]
		if p.Status != traffic.StatusEnabled || p.Sticky == nil || p.TargetVersionID == nil {
			continue
		}
		name := p.Sticky.CookieName
		if name == "" {
			name = "MAPLE_SRV"
		}
		if raw := rv.Header.Get("Cookie"); raw != "" {
			if v := cookieValue(raw, name); v != "" {
				return "c:" + v
			}
		}
	}
	if rv.ClientIP != "" {
		return "ip:" + rv.ClientIP
	}
	return ""
}

// hashBucket 返回 key 的哈希值映射到 0-99 的桶，用于 percent 抽签。
func hashBucket(key string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return int(h.Sum32() % 100)
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
			if r.metrics != nil {
				r.metrics.Inc("maple_rate_limit_hits_total",
					map[string]string{"scope": string(l.Scope)})
			}
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

// pickSticky 处理 sticky 策略的会话保持。
//   - header 会话键存在：一致性哈希选实例（同 header 恒同实例）。
//   - cookie 值存在且命中池内实例：直接钉住该实例。
//   - 均无：选一个实例并把其实例 ID 作为 cookie 值下发（首访建立会话）。
func (r *CacheResolver) pickSticky(e *RouteEntry, p *traffic.Policy, versionID uuid.UUID,
	rv traffic.RequestView) (*router.Target, error) {
	if p.Sticky == nil {
		return r.pickFromVersion(e, versionID)
	}

	pool := r.versionPool(e, versionID)
	if len(pool) == 0 {
		return r.pickFallback(e, versionID)
	}

	// 1) header 会话键：一致性哈希。
	if p.Sticky.HeaderName != "" {
		if v := rv.Header.Get(p.Sticky.HeaderName); v != "" {
			m := stickySelect(pool, "h:"+v)
			return stickyTarget(e, m, nil, versionID), nil
		}
	}

	// 2) cookie 已存在：值是实例 ID，直接命中即钉住；失效则回落。
	name := p.Sticky.CookieName
	if name == "" {
		name = "MAPLE_SRV"
	}
	if raw := rv.Header.Get("Cookie"); raw != "" {
		if id := cookieValue(raw, name); id != "" {
			for i := range pool {
				if pool[i].ID == id {
					return stickyTarget(e, &pool[i], nil, versionID), nil
				}
			}
			// cookie 指向已摘除实例：按权重另选并刷新 cookie。
			return r.pickFromVersionWithSticky(e, p.Sticky, versionID, name)
		}
	}

	// 3) 首访：选实例并下发 cookie（值 = 实例 ID）。
	return r.pickFromVersionWithSticky(e, p.Sticky, versionID, name)
}

// pickFromVersionWithSticky 在版本池内选一个实例，并在 Target 上携带下发 cookie。
func (r *CacheResolver) pickFromVersionWithSticky(e *RouteEntry, s *traffic.Sticky,
	versionID uuid.UUID, cookieName string) (*router.Target, error) {
	pool := r.versionPool(e, versionID)
	if len(pool) == 0 {
		return r.pickFallback(e, versionID)
	}
	m := r.balancer.Select(e.DomainID.String()+"#inst:"+versionID.String(), pool)
	if m == nil {
		return nil, router.ErrNoHealthy
	}
	return stickyTarget(e, m, &router.StickyCookie{
		Name:       cookieName,
		Value:      m.ID,
		TTLSeconds: s.TTLSeconds,
	}, versionID), nil
}

// versionPool 取某版本的健康实例池。
func (r *CacheResolver) versionPool(e *RouteEntry, versionID uuid.UUID) []router.PoolMember {
	for _, v := range e.Versions {
		if v.VersionID == versionID {
			return v.Pool
		}
	}
	return nil
}

// stickyTarget 构造目标；cookie 非空时携带 Set-Cookie 下发信息。
func stickyTarget(e *RouteEntry, m *router.PoolMember, c *router.StickyCookie, versionID uuid.UUID) *router.Target {
	t := targetFromMember(e, m, versionID)
	t.SetSticky = c
	return t
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

// pickFromVersion 从指定版本的健康实例池中选一个实例（默认 round_robin）。
// 该版本无健康实例时回退到其余版本按权重分配（版本自身健康摘除语义）。
func (r *CacheResolver) pickFromVersion(e *RouteEntry, versionID uuid.UUID) (*router.Target, error) {
	return r.pickFromVersionBalanced(e, versionID, traffic.BalanceRoundRobin, traffic.RequestView{})
}

// pickFromVersionBalanced 按 balance 从版本池中选实例：
//   - round_robin：平滑加权轮询（默认，与既有行为一致）；
//   - consistent_hash：用会话键（优先 sticky header / cookie / IP）一致性哈希钉实例；
//   - least_conn：选在途连接最少实例。
//
// 该版本无健康实例时回退到其余版本按权重分配（版本自身健康摘除语义）。
func (r *CacheResolver) pickFromVersionBalanced(e *RouteEntry, versionID uuid.UUID,
	balance traffic.Balance, rv traffic.RequestView) (*router.Target, error) {
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

	var m *router.PoolMember
	switch balance {
	case traffic.BalanceConsistentHash:
		if key := r.sessionKey(e, rv); key != "" {
			m = loadbalancer.ConsistentHash(pool, key)
		}
		if m == nil {
			// 无会话键：退化为 RR（保持可用）。
			m = r.balancer.Select(e.DomainID.String()+"#inst:"+versionID.String(), pool)
		}
	case traffic.BalanceLeastConnection:
		m = r.balancer.SelectLeastConnections(pool)
	default:
		m = r.balancer.Select(e.DomainID.String()+"#inst:"+versionID.String(), pool)
	}
	if m == nil {
		return nil, router.ErrNoHealthy
	}
	return targetFromMember(e, m, versionID), nil
}

// sessionKey 构造一致性哈希的会话键：优先命中策略的 sticky header / cookie，兜底客户端 IP。
func (r *CacheResolver) sessionKey(e *RouteEntry, rv traffic.RequestView) string {
	// 命中策略若声明 sticky header，优先用它（与会话一致性同源）。
	for i := range e.Policies {
		p := &e.Policies[i]
		if p.Status != traffic.StatusEnabled || p.Sticky == nil || p.Sticky.HeaderName == "" {
			continue
		}
		if v := rv.Header.Get(p.Sticky.HeaderName); v != "" {
			return "h:" + v
		}
	}
	if raw := rv.Header.Get("Cookie"); raw != "" {
		if v := cookieValue(raw, "MAPLE_SRV"); v != "" {
			return "c:" + v
		}
	}
	if rv.ClientIP != "" {
		return "ip:" + rv.ClientIP
	}
	return ""
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
		m := p.Match
		// 无显式条件的策略（空 match 或仅 percent）会恒命中所有请求，
		// 交由 percent/权重兜底路径处理，不在此处抢占。
		if len(m.Header) == 0 && len(m.Cookie) == 0 && m.Path == "" && m.PathExact == "" {
			continue
		}
		if !p.Matches(rv) {
			continue
		}
		if best == nil || p.Priority < best.Priority {
			best = p
		}
	}
	return best
}

func targetFromMember(e *RouteEntry, m *router.PoolMember, versionID uuid.UUID) *router.Target {
	scheme := m.Protocol
	if scheme == "" {
		scheme = e.Protocol
	}
	if scheme == "" {
		scheme = "http"
	}
	return &router.Target{Scheme: scheme, Host: m.Endpoint, InstanceID: m.ID, VersionID: versionID}
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
