package cache

import (
	"context"

	"github.com/NeoPlayful/maple-gateway/server/internal/loadbalancer"
	"github.com/NeoPlayful/maple-gateway/server/internal/router"
)

// CacheResolver 把路由缓存接入 router.Resolver。
//
// 每个 Host 命中后，用 Load Balancer 从该路由的健康实例池中选择目标。
type CacheResolver struct {
	cache    *Cache
	balancer *loadbalancer.Balancer
}

// NewResolver 构造。
func NewResolver(c *Cache) *CacheResolver {
	return &CacheResolver{
		cache:    c,
		balancer: loadbalancer.NewBalancer(),
	}
}

// Resolve 实现 router.Resolver：Host → 目标实例。
func (r *CacheResolver) Resolve(_ context.Context, host string) (*router.Target, error) {
	e := r.cache.Lookup(host)
	if e == nil {
		return nil, router.ErrNotFound
	}
	if !e.TenantOK {
		return nil, router.ErrTenantSuspended
	}
	m := r.balancer.Select(e.DomainID.String(), e.Pool)
	if m == nil {
		return nil, router.ErrNoHealthy
	}
	scheme := m.Protocol
	if scheme == "" {
		scheme = e.Protocol
	}
	if scheme == "" {
		scheme = "http"
	}
	return &router.Target{Scheme: scheme, Host: m.Endpoint}, nil
}
