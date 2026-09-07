// Package gateway 负责数据平面生命周期：组装依赖、启动监听、优雅关闭。
package gateway

import (
	"net/http"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/internal/logs"
	"github.com/NeoPlayful/maple-gateway/server/internal/metrics"
	"github.com/NeoPlayful/maple-gateway/server/internal/proxy"
	"github.com/NeoPlayful/maple-gateway/server/internal/router"
	"go.uber.org/zap"
)

// listener 描述一个数据平面监听（HTTP 或 HTTPS）。
type listener struct {
	server *http.Server
	tls    bool // true 表示用 ListenAndServeTLS(cert,key)
	cert   string
	key    string
}

// DataPlane 把数据平面 HTTP(S) 服务封装起来，支持 HTTP/HTTPS 并行监听。
type DataPlane struct {
	listeners []listener
	logger    *zap.Logger
}

// DataPlaneConfig 数据平面依赖。
type DataPlaneConfig struct {
	Address           string // HTTP 监听地址，如 ":80"
	HTTPSAddress      string // HTTPS 监听地址（空 = 不启用 HTTPS）
	CertFile          string // TLS 证书路径
	KeyFile           string // TLS 私钥路径
	Resolver          router.Resolver
	Transport         *http.Transport
	ReadHeaderTimeout time.Duration
	IdleTimeout       time.Duration
	MaxHeaderBytes    int
	Logger            *zap.Logger
	Metrics           *metrics.Registry // 可空；nil 时不采集指标
	AccessLog         *logs.AccessLog   // 可空；nil 时不记录访问日志
}

// NewDataPlane 组装数据平面服务（不启动）。Address 与 HTTPSAddress 至少其一非空。
func NewDataPlane(cfg DataPlaneConfig) *DataPlane {
	px := proxy.New(proxy.Config{
		Resolver:  cfg.Resolver,
		Transport: cfg.Transport,
		Logger:    cfg.Logger,
		Metrics:   cfg.Metrics,
		AccessLog: cfg.AccessLog,
	})
	handler := http.Handler(px)

	d := &DataPlane{logger: cfg.Logger}
	newServer := func(addr string) *http.Server {
		return &http.Server{
			Addr:              addr,
			Handler:           handler,
			ReadHeaderTimeout: cfg.ReadHeaderTimeout,
			IdleTimeout:       cfg.IdleTimeout,
			MaxHeaderBytes:    cfg.MaxHeaderBytes,
		}
	}
	if cfg.Address != "" {
		d.listeners = append(d.listeners, listener{server: newServer(cfg.Address)})
	}
	if cfg.HTTPSAddress != "" {
		d.listeners = append(d.listeners, listener{
			server: newServer(cfg.HTTPSAddress),
			tls:    true,
			cert:   cfg.CertFile,
			key:    cfg.KeyFile,
		})
	}
	return d
}
