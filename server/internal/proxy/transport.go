// Package proxy 是 Reverse Proxy 数据平面核心。
package proxy

import (
	"crypto/tls"
	"net"
	"net/http"
	"time"
)

// TransportConfig 配置 upstream 连接行为。
type TransportConfig struct {
	ResponseHeaderTimeout time.Duration
	IdleTimeout           time.Duration
	MaxIdleConns          int
	MaxIdleConnsPerHost   int
}

// NewTransport 构建可复用的 upstream http.Transport。
// 支持 h2c 升级、TLS 上游、连接复用。
func NewTransport(cfg TransportConfig) *http.Transport {
	return &http.Transport{
		// 每个请求的连接可复用，避免频繁建连。
		MaxIdleConns:          cfg.MaxIdleConns,
		MaxIdleConnsPerHost:   cfg.MaxIdleConnsPerHost,
		IdleConnTimeout:       cfg.IdleTimeout,
		ResponseHeaderTimeout: cfg.ResponseHeaderTimeout,
		// 允许 HTTP/2 Cleartext（h2c）upstream。
		ForceAttemptHTTP2: true,
		// TLS 上游证书校验默认开启。
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
	}
}
