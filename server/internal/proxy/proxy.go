package proxy

import (
	"errors"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/internal/router"
	"go.uber.org/zap"
)

// Logger 供日志注入的最小接口（*zap.Logger 天然满足）。
type Logger interface {
	Info(msg string, fields ...zap.Field)
	Warn(msg string, fields ...zap.Field)
	Error(msg string, fields ...zap.Field)
}

// Config 组装 Proxy 所需依赖。
type Config struct {
	Resolver  router.Resolver
	Transport *http.Transport
	Logger    Logger
}

// Proxy 是数据平面反向代理。
type Proxy struct {
	resolver router.Resolver
	director *httputil.ReverseProxy
	logger   Logger
}

// New 构造 Proxy。transport 为空时使用默认配置。
func New(cfg Config) *Proxy {
	transport := cfg.Transport
	if transport == nil {
		transport = NewTransport(TransportConfig{
			ResponseHeaderTimeout: 60 * time.Second,
			IdleTimeout:           120 * time.Second,
			MaxIdleConns:          200,
			MaxIdleConnsPerHost:   20,
		})
	}

	p := &Proxy{
		resolver: cfg.Resolver,
		logger:   cfg.Logger,
	}
	p.director = &httputil.ReverseProxy{
		Transport:     transport,
		FlushInterval: -1, // 立即 flush，保证 SSE / 流式低延迟
		Rewrite:       p.rewrite,
		ErrorHandler:  p.handleUpstreamError,
	}
	return p
}

// ServeHTTP 实现 http.Handler，数据平面入口。
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Host 规范化：畸形 Host 直接拒绝（400），不进入路由。
	host, err := router.NormalizeHost(r.Host)
	if err != nil {
		if p.logger != nil {
			p.logger.Warn("invalid host rejected", zap.String("host", r.Host))
		}
		http.Error(w, "invalid host", http.StatusBadRequest)
		return
	}

	target, err := p.resolver.Resolve(r.Context(), host)
	if err != nil {
		p.handleResolveError(w, r, err)
		return
	}

	r = r.WithContext(withTarget(r.Context(), target))
	p.director.ServeHTTP(w, r)
}

// rewrite 是 ReverseProxy 的 URL / Header 改写钩子。
func (p *Proxy) rewrite(pr *httputil.ProxyRequest) {
	target := targetFromContext(pr.In.Context())
	if target == nil {
		return
	}
	pr.SetURL(mustParse(target.URL()))
	applyForwardedHeaders(pr.Out.Header, pr.In.RemoteAddr, pr.In.TLS != nil)
	// 主机头默认透传原始 Host（S6 提供按策略重写）。
	pr.Out.Host = pr.In.Host
}

func (p *Proxy) handleResolveError(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, router.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, router.ErrDomainDisabled):
		status = http.StatusForbidden
	case errors.Is(err, router.ErrTenantSuspended), errors.Is(err, router.ErrNoHealthy):
		status = http.StatusServiceUnavailable
	}
	if p.logger != nil {
		p.logger.Warn("route resolve rejected",
			zap.String("host", r.Host),
			zap.String("err", err.Error()),
			zap.Int("status", status),
		)
	}
	http.Error(w, http.StatusText(status), status)
}

func (p *Proxy) handleUpstreamError(w http.ResponseWriter, r *http.Request, err error) {
	if p.logger != nil {
		p.logger.Error("upstream error",
			zap.String("host", r.Host),
			zap.String("err", err.Error()),
		)
	}
	http.Error(w, http.StatusText(http.StatusBadGateway), http.StatusBadGateway)
}

func mustParse(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		panic("proxy: invalid target url: " + raw)
	}
	return u
}
