package certificate

import (
	"crypto/tls"
	"errors"
	"sync/atomic"

	"github.com/NeoPlayful/maple-gateway/server/internal/router"
)

// 握手期错误（返回给 tls 握手层，表现为 TLS alert；不携带敏感信息）。
var (
	// ErrUnknownSNI 表示 ServerName 无对应证书且未启用回退。
	ErrUnknownSNI = errors.New("no certificate for server name")
	// ErrNoSNI 表示 ClientHello 未带 ServerName 且未启用回退（裸 IP / 非 SNI 客户端）。
	ErrNoSNI = errors.New("client hello without server name")
)

// Getter 实现 tls.Config.GetCertificate 语义，纯内存缓存查找（握手热路径绝不访问 DB）。
//
// 遵循 Go tls 契约：返回 (nil, nil) 时库会回退到 tls.Config.Certificates[0]。
// 因此 fallback=true 时，SNI 未命中 / 无 SNI 返回 (nil, nil)，让上层全局证书兜底；
// fallback=false 时返回错误直接拒绝握手（故障隔离）。
type Getter struct {
	cache    *Cache
	fallback bool
	onMiss   func(serverName string, usedFallback bool) // 日志/指标钩子，可空
	hits     atomic.Uint64                              // 缓存命中次数
	misses   atomic.Uint64                              // 缓存未命中（含无 SNI）次数
}

// NewGetter 构造 SNI getter。
// onMiss 在每个未命中（含无 SNI）时调用一次，用于计数与日志，禁止在钩子内访问 DB。
func NewGetter(cache *Cache, fallback bool, onMiss func(serverName string, usedFallback bool)) *Getter {
	return &Getter{cache: cache, fallback: fallback, onMiss: onMiss}
}

// GetCertificate 返回匹配 ClientHello.ServerName 的证书。并发安全。
func (g *Getter) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	if g == nil || hello == nil {
		return nil, ErrNoSNI
	}
	sn, err := router.NormalizeHost(hello.ServerName)
	if err != nil {
		sn = ""
	}
	if sn != "" {
		if loaded := g.cache.Get(sn); loaded != nil && loaded.Cert != nil {
			g.hits.Add(1)
			return loaded.Cert, nil
		}
	}
	// 未命中 / 无 SNI：按回退策略处理。
	g.misses.Add(1)
	if g.onMiss != nil {
		g.onMiss(sn, g.fallback)
	}
	if g.fallback {
		// 让 tls 库回退到 tls.Config.Certificates[0]。
		return nil, nil
	}
	if sn == "" {
		return nil, ErrNoSNI
	}
	return nil, ErrUnknownSNI
}

// Len 缓存条目数（运维/指标用）。
func (g *Getter) Len() int {
	if g == nil || g.cache == nil {
		return 0
	}
	return g.cache.Len()
}

// Stats 返回命中/未中计数（Prometheus 采样或运维接口用）。
func (g *Getter) Stats() (hits, misses uint64) {
	if g == nil {
		return 0, 0
	}
	return g.hits.Load(), g.misses.Load()
}
