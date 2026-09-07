// Package gateway 负责数据平面生命周期：组装依赖、启动监听、优雅关闭。
package gateway

import (
	"net/http"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/internal/proxy"
	"github.com/NeoPlayful/maple-gateway/server/internal/router"
	"go.uber.org/zap"
)

// DataPlane 把数据平面 HTTP 服务封装起来。
type DataPlane struct {
	httpServer *http.Server
	logger     *zap.Logger
}

// DataPlaneConfig 数据平面依赖。
type DataPlaneConfig struct {
	Address           string // 监听地址，如 ":80"
	Resolver          router.Resolver
	Transport         *http.Transport
	ReadHeaderTimeout time.Duration
	IdleTimeout       time.Duration
	MaxHeaderBytes    int
	Logger            *zap.Logger
}

// NewDataPlane 组装数据平面服务（不启动）。
func NewDataPlane(cfg DataPlaneConfig) *DataPlane {
	px := proxy.New(proxy.Config{
		Resolver:  cfg.Resolver,
		Transport: cfg.Transport,
		Logger:    cfg.Logger,
	})

	srv := &http.Server{
		Addr:              cfg.Address,
		Handler:           px,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		MaxHeaderBytes:    cfg.MaxHeaderBytes,
	}
	return &DataPlane{httpServer: srv, logger: cfg.Logger}
}
