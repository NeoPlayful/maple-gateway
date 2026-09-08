package proxy

import (
	"bufio"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/internal/logs"
	"github.com/NeoPlayful/maple-gateway/server/internal/metrics"
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
	Metrics   *metrics.Registry // 可空；nil 时不采集数据平面指标
	AccessLog *logs.AccessLog   // 可空；nil 时不记录访问日志
	ErrLog    *logs.ErrLog      // 可空；nil 时不记录错误日志
}

// Proxy 是数据平面反向代理。
type Proxy struct {
	resolver router.Resolver
	director *httputil.ReverseProxy
	logger   Logger
	metrics  *metrics.Registry
	access   *logs.AccessLog
	errLog   *logs.ErrLog
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
		metrics:  cfg.Metrics,
		access:   cfg.AccessLog,
		errLog:   cfg.ErrLog,
	}
	p.director = &httputil.ReverseProxy{
		Transport:     transport,
		FlushInterval: -1, // 立即 flush，保证 SSE / 流式低延迟
		Rewrite:       p.rewrite,
		ErrorHandler:  p.handleUpstreamError,
		ModifyResponse: func(resp *http.Response) error {
			if p.metrics != nil {
				p.metrics.Inc("maple_upstream_requests_total", map[string]string{
					"host": normalizeHostLabel(resp.Request.Host),
				})
			}
			// sticky 首访：把会话 cookie 随首个响应下发，供后续请求钉住同实例。
			if sc := stickyFromContext(resp.Request.Context()); sc != nil {
				ttl := sc.TTLSeconds
				if ttl <= 0 {
					ttl = 86400 * 30 // 默认 30 天
				}
				cookie := (&http.Cookie{
					Name:   sc.Name,
					Value:  sc.Value,
					Path:   "/",
					MaxAge: ttl,
				}).String()
				resp.Header.Add("Set-Cookie", cookie)
			}
			return nil
		},
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

	if p.metrics != nil {
		p.metrics.Inc("maple_active_connections", nil)
		defer p.metrics.Add("maple_active_connections", -1, nil)
	}
	start := time.Now()

	target, err := p.resolve(host, r)
	if err != nil {
		p.handleResolveError(w, r, err)
		return
	}

	rec := &statusRecorder{ResponseWriter: w, status: 200}
	r = r.WithContext(withTarget(r.Context(), target))
	elapsed := time.Since(start)
	p.director.ServeHTTP(rec, r)
	if p.metrics != nil {
		p.metrics.Inc("maple_requests_total", map[string]string{
			"host":   host,
			"status": itoa(rec.status),
		})
		p.metrics.ObserveDuration("maple_request_duration_seconds", elapsed,
			map[string]string{"host": host})
	}
	if p.access != nil {
		p.access.Append(logs.AccessEntry{
			Timestamp:  time.Now(),
			Host:       host,
			Method:     r.Method,
			Path:       r.URL.Path,
			Status:     rec.status,
			ClientIP:   clientIP(r.RemoteAddr),
			DurationMS: elapsed.Milliseconds(),
		})
	}
}

func normalizeHostLabel(host string) string {
	n, err := router.NormalizeHost(host)
	if err != nil {
		return host
	}
	return n
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		b[pos] = '-'
	}
	return string(b[pos:])
}

// statusRecorder 捕获上游响应状态码供指标/访问日志使用。
// 需透传 Flush/Hijack/ReadFrom 等可选接口，保证 ReverseProxy 流式与升级正常。
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = 200
	}
	return s.ResponseWriter.Write(b)
}

func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := s.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, errors.New("statusRecorder: underlying writer does not support hijacking")
}

func (s *statusRecorder) ReadFrom(r io.Reader) (int64, error) {
	if rf, ok := s.ResponseWriter.(io.ReaderFrom); ok {
		return rf.ReadFrom(r)
	}
	return io.Copy(struct{ io.Writer }{s.ResponseWriter}, r)
}

func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// resolve 优先走支持请求上下文的 resolver（策略分流），否则退化为无上下文 Resolve。
func (p *Proxy) resolve(host string, r *http.Request) (*router.Target, error) {
	if cr, ok := p.resolver.(router.ContextResolver); ok {
		return cr.ResolveWith(r.Context(), host, router.MatchView{
			Header:   headerMap(r.Header),
			Path:     r.URL.Path,
			ClientIP: clientIP(r.RemoteAddr),
		})
	}
	return p.resolver.Resolve(r.Context(), host)
}

// headerMap 取请求头首个值，键小写（匹配规则按小写键精确匹配）。
func headerMap(h http.Header) map[string]string {
	if len(h) == 0 {
		return nil
	}
	out := make(map[string]string, len(h))
	for k, vv := range h {
		if len(vv) > 0 {
			out[strings.ToLower(k)] = vv[0]
		}
	}
	return out
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
	case errors.Is(err, router.ErrRateLimited):
		status = http.StatusTooManyRequests
		w.Header().Set("Retry-After", "1")
	}
	p.countRequest(r, status)
	if p.logger != nil {
		p.logger.Warn("route resolve rejected",
			zap.String("host", r.Host),
			zap.String("err", err.Error()),
			zap.Int("status", status),
		)
	}
	p.appendErr(r, status, err.Error())
	http.Error(w, http.StatusText(status), status)
}

func (p *Proxy) handleUpstreamError(w http.ResponseWriter, r *http.Request, err error) {
	// 上游 502 由 statusRecorder 经 129 行统一计数，这里不重复计。
	if p.logger != nil {
		p.logger.Error("upstream error",
			zap.String("host", r.Host),
			zap.String("err", err.Error()),
		)
	}
	p.appendErr(r, http.StatusBadGateway, err.Error())
	http.Error(w, http.StatusText(http.StatusBadGateway), http.StatusBadGateway)
}

// countRequest 按真实响应状态累计请求计数（错误分支：路由拒绝 / 上游失败）。
func (p *Proxy) countRequest(r *http.Request, status int) {
	if p.metrics == nil {
		return
	}
	host := normalizeHostLabel(r.Host)
	if host == "" {
		host = "-"
	}
	p.metrics.Inc("maple_requests_total", map[string]string{
		"host":   host,
		"status": itoa(status),
	})
}

// appendErr 写入错误日志缓冲（若启用）。
func (p *Proxy) appendErr(r *http.Request, status int, msg string) {
	if p.errLog == nil {
		return
	}
	p.errLog.Append(logs.ErrEntry{
		Timestamp: time.Now(),
		Host:      normalizeHostLabel(r.Host),
		Path:      r.URL.Path,
		Status:    status,
		Error:     msg,
	})
}

func mustParse(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		panic("proxy: invalid target url: " + raw)
	}
	return u
}
