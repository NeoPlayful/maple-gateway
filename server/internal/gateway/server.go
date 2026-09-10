// Package gateway 负责数据平面生命周期：组装依赖、启动监听、优雅关闭。
package gateway

import (
	"crypto/tls"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/internal/logs"
	"github.com/NeoPlayful/maple-gateway/server/internal/metrics"
	"github.com/NeoPlayful/maple-gateway/server/internal/proxy"
	"github.com/NeoPlayful/maple-gateway/server/internal/router"
	"github.com/NeoPlayful/maple-gateway/server/internal/tracex"
	"go.uber.org/zap"
)

// TLSCertGetter 抽象 SNI 动态选证书（direct 模式注入）。
type TLSCertGetter = func(*tls.ClientHelloInfo) (*tls.Certificate, error)

// TLSMode 数据平面 HTTPS 证书选择模式。
type TLSMode string

const (
	// TLSModeGlobal 用单全局静态证书（Phase 4 行为，回归基线）。
	TLSModeGlobal TLSMode = "global"
	// TLSModeDirect 用 tls.Config.GetCertificate 按 SNI 动态选证书。
	TLSModeDirect TLSMode = "direct"
)

// listener 描述一个数据平面监听（HTTP 或 HTTPS）。
type listener struct {
	server  *http.Server
	tls     bool // true 表示 TLS 监听
	tlsMode TLSMode
}

// DataPlane 把数据平面 HTTP(S) 服务封装起来，支持 HTTP/HTTPS 并行监听。
type DataPlane struct {
	listeners []listener
	logger    *zap.Logger
}

// DataPlaneConfig 数据平面依赖。
type DataPlaneConfig struct {
	Address             string // HTTP 监听地址，如 ":80"
	HTTPSAddress        string // HTTPS 监听地址（空 = 不启用 HTTPS）
	CertFile            string // TLS 证书路径（global 主证书 / direct 回退证书）
	KeyFile             string // TLS 私钥路径
	TLSMode             TLSMode
	TLSMinVersion       string        // tls1.2 / tls1.3
	GetCertificate      TLSCertGetter // direct 模式动态 SNI 选证书；可空
	FallbackCertOnMiss  bool          // direct 模式：未知 SNI/无 SNI 用回退证书兜底
	EnforceSNIHostMatch bool          // direct 模式：SNI 与 Host 不一致返回 421
	Resolver            router.Resolver
	Transport           *http.Transport
	ReadHeaderTimeout   time.Duration
	IdleTimeout         time.Duration
	MaxHeaderBytes      int
	Logger              *zap.Logger
	Metrics             *metrics.Registry // 可空；nil 时不采集指标
	AccessLog           *logs.AccessLog   // 可空；nil 时不记录访问日志
	ErrLog              *logs.ErrLog      // 可空；nil 时不记录错误日志
	Tracer              tracex.Tracer     // 可空；nil 时不埋 OTel trace
}

// NewDataPlane 组装数据平面服务（不启动）。Address 与 HTTPSAddress 至少其一非空。
func NewDataPlane(cfg DataPlaneConfig) *DataPlane {
	px := proxy.New(proxy.Config{
		Resolver:            cfg.Resolver,
		Transport:           cfg.Transport,
		Logger:              cfg.Logger,
		Metrics:             cfg.Metrics,
		AccessLog:           cfg.AccessLog,
		ErrLog:              cfg.ErrLog,
		Tracer:              cfg.Tracer,
		EnforceSNIHostMatch: cfg.EnforceSNIHostMatch,
	})
	handler := http.Handler(px)

	d := &DataPlane{logger: cfg.Logger}
	newServer := func(addr string, tlsCfg *tls.Config) *http.Server {
		return &http.Server{
			Addr:              addr,
			Handler:           handler,
			ReadHeaderTimeout: cfg.ReadHeaderTimeout,
			IdleTimeout:       cfg.IdleTimeout,
			MaxHeaderBytes:    cfg.MaxHeaderBytes,
			TLSConfig:         tlsCfg,
			// net/http 默认用标准库 log 打握手错误（格式不统一、不受日志级别控制）；
			// 收编进 zap：SNI 哨兵错误由 onMiss 记录后此处丢弃，其余按 Debug。
			ErrorLog: log.New(newZapErrorLog(cfg.Logger), "", 0),
		}
	}

	// 回退证书（global 主证书 / direct 兜底共用）：cert/key 非空则加载。
	haveCert := cfg.CertFile != "" && cfg.KeyFile != ""
	loadPair := func() (tls.Certificate, bool) {
		if !haveCert {
			return tls.Certificate{}, false
		}
		c, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			cfg.Logger.Warn("load tls cert failed",
				zap.String("cert", cfg.CertFile), zap.Error(err))
			return tls.Certificate{}, false
		}
		return c, true
	}

	if cfg.Address != "" {
		d.listeners = append(d.listeners, listener{server: newServer(cfg.Address, nil)})
	}

	if cfg.HTTPSAddress != "" {
		ln := listener{server: newServer(cfg.HTTPSAddress, nil), tls: true}

		switch cfg.TLSMode {
		case TLSModeDirect:
			// Direct TLS 证书源只有 DB（经 GetCertificate）。config 的静态证书（cert/key）
			// 是 global 模式的，绝不装进 direct 的 tls.Config——任何时刻都不出现
			// "一张证书服务一堆域名"的共用形态。
			ln.tlsMode = TLSModeDirect
			tlsCfg := &tls.Config{MinVersion: minTLSVersion(cfg.TLSMinVersion)}
			if cfg.GetCertificate != nil {
				tlsCfg.GetCertificate = cfg.GetCertificate
			} else {
				// 证书源缺失（无 DB / 无 MAPLE_CERT_ENC_KEY）：不退出进程，但 HTTPS
				// 全部拒绝握手（B 方案）——注入恒拒占位，避免 tls 库因无证书可发而
				// 直接使监听不可用或落入全局证书兜底。HTTP/管理面照常。
				tlsCfg.GetCertificate = rejectAllGetter
				cfg.Logger.Warn("direct mode without a certificate source (database/cert service down): " +
					"all TLS handshakes will be rejected")
			}
			ln.server.TLSConfig = tlsCfg
		default: // global
			ln.tlsMode = TLSModeGlobal
			fallback, ok := loadPair()
			if !ok {
				cfg.Logger.Warn("https enabled in global mode but cert/key missing or unreadable")
			} else {
				ln.server.TLSConfig = &tls.Config{
					MinVersion:   minTLSVersion(cfg.TLSMinVersion),
					Certificates: []tls.Certificate{fallback},
				}
			}
		}
		d.listeners = append(d.listeners, ln)
	}
	return d
}

// rejectAllGetter 是 direct 模式证书源缺失时的恒拒占位：任何握手都返回错误拒绝，
// 不向任何客户端发证书。进程因此继续存活（HTTP/管理面可用），仅 HTTPS 不可用。
func rejectAllGetter(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	return nil, errors.New("direct tls certificate source unavailable")
}

// minTLSVersion 把配置字符串映射为 tls.Version*。
func minTLSVersion(v string) uint16 {
	switch v {
	case "tls1.3":
		return tls.VersionTLS13
	default:
		return tls.VersionTLS12
	}
}
