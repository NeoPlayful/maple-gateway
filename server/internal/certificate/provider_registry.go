package certificate

import (
	"fmt"
	"sync"
)

// ProviderRegistry 按证书来源枚举管理 provider。
// 本期注册 manual 与 acme；cloudflare 不注册（Get 返回未命中 → ErrProviderUnavailable）。
type ProviderRegistry struct {
	mu    sync.RWMutex
	bySrc map[Source]CertificateProvider
}

// NewProviderRegistry 构造空注册表。
func NewProviderRegistry() *ProviderRegistry {
	return &ProviderRegistry{bySrc: make(map[Source]CertificateProvider)}
}

// Register 注册/覆盖某来源的 provider（Name() 即来源标识）。
func (r *ProviderRegistry) Register(p CertificateProvider) {
	if p == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.bySrc[Source(p.Name())] = p
}

// Get 按来源取 provider；未注册返回 false。
func (r *ProviderRegistry) Get(src Source) (CertificateProvider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.bySrc[src]
	return p, ok
}

// For 按来源取 provider；未注册时返回包装 ErrProviderUnavailable 的错误。
func (r *ProviderRegistry) For(src Source) (CertificateProvider, error) {
	p, ok := r.Get(src)
	if !ok {
		return nil, fmt.Errorf("%w: source=%s", ErrProviderUnavailable, src)
	}
	return p, nil
}
