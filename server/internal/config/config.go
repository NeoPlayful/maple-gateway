// Package config 负责启动配置的加载与合并。
// 优先级：命令行默认值 < YAML 文件 < 环境变量(MAPLE_*)。
package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

// Config 是完整启动配置。
type Config struct {
	// Listen 全局监听配置：唯一 host（监听网卡） + 各监听端口。
	// 语义变更：旧的 gateway.http/https、management 各自 address: ":port"
	// 改为 listen 提供唯一 host，gateway/management 只保留 port。
	Listen     ListenConfig     `yaml:"listen"`
	Gateway    GatewayConfig    `yaml:"gateway"`
	Management ManagementConfig `yaml:"management"`
	Health     HealthConfig     `yaml:"health"`
	Proxy      ProxyConfig      `yaml:"proxy"`
	Database   DatabaseConfig   `yaml:"database"`
	Redis      RedisConfig      `yaml:"redis"`
	RateLimit  RateLimitConfig  `yaml:"ratelimit"`
	Logging    LoggingConfig    `yaml:"logging"`
	Security   SecurityConfig   `yaml:"security"`
	HA         HAConfig         `yaml:"ha"`
	CanaryAuto CanaryAutoConfig `yaml:"canary_auto"`
	Trace      TraceConfig      `yaml:"trace"`
	TLS        TLSConfig        `yaml:"tls"`
}

// ListenConfig 全局监听网卡配置：所有监听口共用同一 host，各自只配端口。
type ListenConfig struct {
	// Host 绑定网卡地址：空（缺省）或 "" 等价旧 `:port` 写法，绑定本机全部网卡；
	// 显式 0.0.0.0 同理（全接口），127.0.0.1 则仅回环。
	Host string `yaml:"host"`
}

// Config.Addr 用全局 listen.host 拼装完整监听地址（host 空 → JoinHostPort 输出 :port）。
func (c *Config) Addr(port int) string {
	if port == 0 {
		return ""
	}
	return net.JoinHostPort(c.Listen.Host, strconv.Itoa(port))
}

// TraceConfig 是 OpenTelemetry 数据面追踪配置。
type TraceConfig struct {
	Enabled     bool    `yaml:"enabled"`      // 是否开启数据面 span 采集与导出
	SampleRatio float64 `yaml:"sample_ratio"` // 采样率 0-1；0 默认关闭采样（全量导出太吵）
}

type GatewayConfig struct {
	HTTP  HTTPListener `yaml:"http"`
	HTTPS HTTPListener `yaml:"https"`
}

type HTTPListener struct {
	Port    int    `yaml:"port"` // 监听端口（host 见顶层 listen.host）
	Enabled bool   `yaml:"enabled"`
	Cert    string `yaml:"cert"` // TLS 证书路径（Phase 2）
	Key     string `yaml:"key"`  // TLS 私钥路径（Phase 2）
}

type ManagementConfig struct {
	Port int `yaml:"port"` // 管理面端口（host 见顶层 listen.host）
	// UIDir 管理后台前端构建产物目录（dist）；空则不托管 UI。
	UIDir string `yaml:"ui_dir"`
}

type HealthConfig struct {
	Interval         time.Duration `yaml:"interval"`
	Timeout          time.Duration `yaml:"timeout"`
	FailureThreshold int           `yaml:"failure_threshold"`
	SuccessThreshold int           `yaml:"success_threshold"`
	GracePeriod      time.Duration `yaml:"grace_period"`
}

type ProxyConfig struct {
	ReadHeaderTimeout     time.Duration `yaml:"read_header_timeout"`
	ResponseHeaderTimeout time.Duration `yaml:"response_header_timeout"`
	IdleTimeout           time.Duration `yaml:"idle_timeout"`
	MaxHeaderBytes        int           `yaml:"max_header_bytes"`
	MaxBodyBytes          int64         `yaml:"max_body_bytes"`
}

type DatabaseConfig struct {
	Driver string `yaml:"driver"`
	URL    string `yaml:"url"`
}

type RedisConfig struct {
	Enabled bool   `yaml:"enabled"`
	URL     string `yaml:"url"`
	Prefix  string `yaml:"prefix"` // 业务 key 命名空间前缀；空则用 maple
}

type LoggingConfig struct {
	Level  string `yaml:"level"`
	Pretty bool   `yaml:"pretty"`
}

type SecurityConfig struct {
	AdminToken           string   `yaml:"admin_token"`
	TrustedProxies       []string `yaml:"trusted_proxies"`
	InstanceAllowPrivate bool     `yaml:"instance_allow_private"`
}

// RateLimitConfig 是限流后端配置。
type RateLimitConfig struct {
	Mode string `yaml:"mode"` // memory（默认单机）/ redis（跨实例共享）
}

// CanaryAutoConfig 是自动 Canary 执行器配置。
type CanaryAutoConfig struct {
	Enabled      bool          `yaml:"enabled"`        // 是否启用指标驱动自动推进/回滚
	Interval     time.Duration `yaml:"interval"`       // 评估周期
	ErrRateMax   float64       `yaml:"err_rate_max"`   // canary 错误率上限（%），超限自动回滚
	ErrLatencyMS float64       `yaml:"err_latency_ms"` // 平均延迟上限（ms），>0 才启用
	MinRequests  int64         `yaml:"min_requests"`   // 窗口最小请求数，不足不推进
}

// HAConfig 是本 Gateway 进程的多实例协调配置。
type HAConfig struct {
	Enabled    bool          `yaml:"enabled"`     // 是否参与 Leader 竞逐/多实例协调
	InstanceID string        `yaml:"instance_id"` // 唯一实例标识；空则自动生成
	Heartbeat  time.Duration `yaml:"heartbeat"`   // 心跳周期
	LeaseTTL   time.Duration `yaml:"lease_ttl"`   // lease 时长
}

// TLSConfig 是数据平面 TLS 接入模式配置（Phase 5 Direct TLS）。
type TLSConfig struct {
	// Mode: global（单全局证书，Phase 4 行为）/ direct（每域名动态 SNI 证书）。
	Mode string `yaml:"mode"`
	// MinVersion 允许 "tls1.2"/"tls1.3"；空默认 tls1.2。
	MinVersion string `yaml:"min_version"`
	// EnforceSNIHostMatch Direct TLS 下 SNI 与 Host 不一致时返回 421。
	EnforceSNIHostMatch bool `yaml:"enforce_sni_host_match"`
	// FallbackCertEnabled direct 模式未知 SNI / 无 SNI（裸 IP、健康检查）用全局回退证书兜底。
	// 默认 false（隔离优先）：未知域名直接拒绝握手；需要内网泛兜底时显式开启。
	FallbackCertEnabled bool `yaml:"fallback_cert_enabled"`
	// CertEncKey 证书私钥存储加密密钥（base64 的 32 字节，AES-256-GCM）。
	// 优先级高于环境变量 MAPLE_CERT_ENC_KEY；两者都不配则证书管理服务禁用
	// （Direct TLS 私钥加密不可降级为明文落库）。
	CertEncKey string `yaml:"cert_enc_key"`
}

// Default 返回内建默认配置（作为 env / 缺省兜底）。
func Default() *Config {
	return &Config{
		// Listen.Host 默认空 = 全接口监听（等价旧 ":port" 写法）。
		Listen: ListenConfig{},
		Gateway: GatewayConfig{
			HTTP: HTTPListener{Port: 8000, Enabled: true},
		},
		Management: ManagementConfig{Port: 4000},
		Health: HealthConfig{
			Interval:         10 * time.Second,
			Timeout:          3 * time.Second,
			FailureThreshold: 3,
			SuccessThreshold: 2,
			GracePeriod:      10 * time.Second,
		},
		Proxy: ProxyConfig{
			ReadHeaderTimeout:     10 * time.Second,
			ResponseHeaderTimeout: 60 * time.Second,
			IdleTimeout:           120 * time.Second,
			MaxHeaderBytes:        1 << 20,
			MaxBodyBytes:          10 << 20,
		},
		RateLimit: RateLimitConfig{Mode: "memory"},
		Logging:   LoggingConfig{Level: "info"},
		Security: SecurityConfig{
			AdminToken:           "",
			InstanceAllowPrivate: true,
		},
		HA: HAConfig{
			Heartbeat: 5 * time.Second,
			LeaseTTL:  15 * time.Second,
		},
		CanaryAuto: CanaryAutoConfig{
			Enabled:     false,
			Interval:    15 * time.Second,
			ErrRateMax:  5,
			MinRequests: 10,
		},
		Trace: TraceConfig{
			Enabled:     false, // 默认关闭：trace 仅显式开启时导出，避免误刷 stdout
			SampleRatio: 0.01,  // 默认低采样 1%
		},
		TLS: TLSConfig{
			Mode:                "direct", // 默认按 SNI 动态选每域名证书（Phase 5）；global 需显式指定单全局证书
			MinVersion:          "tls1.2",
			EnforceSNIHostMatch: true,
			// 默认 false（隔离优先）：未知 SNI/无 SNI 拒绝握手，不向任意域名发全局证书。
			FallbackCertEnabled: false,
		},
	}
}

// Load 从默认值 + 可选 YAML 文件 + 环境变量合并出最终配置。
func Load(path string) (*Config, error) {
	cfg := Default()
	if path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read config file: %w", err)
		}
		if err := yaml.Unmarshal(raw, cfg); err != nil {
			return nil, fmt.Errorf("parse config file: %w", err)
		}
	}
	cfg.applyEnv()
	return cfg, nil
}

// applyEnv 用 MAPLE_* 环境变量覆盖配置项。
func (c *Config) applyEnv() {
	if v := os.Getenv("MAPLE_LISTEN_HOST"); v != "" {
		c.Listen.Host = v
	}
	if v := os.Getenv("MAPLE_GATEWAY_HTTP_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			c.Gateway.HTTP.Port = p
		}
	}
	if v := os.Getenv("MAPLE_MANAGEMENT_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			c.Management.Port = p
		}
	}
	if v := os.Getenv("MAPLE_UI_DIR"); v != "" {
		c.Management.UIDir = v
	}
	if v := os.Getenv("MAPLE_DATABASE_URL"); v != "" {
		c.Database.URL = v
	}
	if v := os.Getenv("MAPLE_REDIS_URL"); v != "" {
		c.Redis.URL = v
	}
	if v := os.Getenv("MAPLE_REDIS_ENABLED"); v != "" {
		c.Redis.Enabled = parseBool(v, c.Redis.Enabled)
	}
	if v := os.Getenv("MAPLE_REDIS_PREFIX"); v != "" {
		c.Redis.Prefix = v
	}
	if v := os.Getenv("MAPLE_ADMIN_TOKEN"); v != "" {
		c.Security.AdminToken = v
	}
	if v := os.Getenv("MAPLE_LOG_LEVEL"); v != "" {
		c.Logging.Level = v
	}
	if v := os.Getenv("MAPLE_GATEWAY_HTTP_ENABLED"); v != "" {
		c.Gateway.HTTP.Enabled = parseBool(v, c.Gateway.HTTP.Enabled)
	}
	if v := os.Getenv("MAPLE_GATEWAY_HTTPS_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			c.Gateway.HTTPS.Port = p
		}
	}
	if v := os.Getenv("MAPLE_GATEWAY_HTTPS_ENABLED"); v != "" {
		c.Gateway.HTTPS.Enabled = parseBool(v, c.Gateway.HTTPS.Enabled)
	}
	if v := os.Getenv("MAPLE_GATEWAY_HTTPS_CERT"); v != "" {
		c.Gateway.HTTPS.Cert = v
	}
	if v := os.Getenv("MAPLE_GATEWAY_HTTPS_KEY"); v != "" {
		c.Gateway.HTTPS.Key = v
	}
	if v := os.Getenv("MAPLE_HA_ENABLED"); v != "" {
		c.HA.Enabled = parseBool(v, c.HA.Enabled)
	}
	if v := os.Getenv("MAPLE_HA_INSTANCE_ID"); v != "" {
		c.HA.InstanceID = v
	}
	if v := os.Getenv("MAPLE_CANARY_AUTO_ENABLED"); v != "" {
		c.CanaryAuto.Enabled = parseBool(v, c.CanaryAuto.Enabled)
	}
	if v := os.Getenv("MAPLE_CANARY_AUTO_ERR_RATE_MAX"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			c.CanaryAuto.ErrRateMax = f
		}
	}
	if v := os.Getenv("MAPLE_RATE_LIMIT_MODE"); v != "" {
		c.RateLimit.Mode = v
	}
	if v := os.Getenv("MAPLE_TRACE_ENABLED"); v != "" {
		c.Trace.Enabled = parseBool(v, c.Trace.Enabled)
	}
	if v := os.Getenv("MAPLE_TRACE_SAMPLE_RATIO"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			c.Trace.SampleRatio = f
		}
	}
	if v := os.Getenv("MAPLE_TLS_MODE"); v != "" {
		c.TLS.Mode = v
	}
	if v := os.Getenv("MAPLE_TLS_MIN_VERSION"); v != "" {
		c.TLS.MinVersion = v
	}
	if v := os.Getenv("MAPLE_TLS_ENFORCE_SNI_HOST_MATCH"); v != "" {
		c.TLS.EnforceSNIHostMatch = parseBool(v, c.TLS.EnforceSNIHostMatch)
	}
	if v := os.Getenv("MAPLE_TLS_FALLBACK_CERT_ENABLED"); v != "" {
		c.TLS.FallbackCertEnabled = parseBool(v, c.TLS.FallbackCertEnabled)
	}
	if v := os.Getenv("MAPLE_TLS_CERT_ENC_KEY"); v != "" {
		c.TLS.CertEncKey = v
	}
}

func parseBool(v string, def bool) bool {
	if b, err := strconv.ParseBool(v); err == nil {
		return b
	}
	return def
}

// validatePort 校验监听端口：缺省(0)或越界时报错。
func validatePort(name string, port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("%s.port must be 1-65535, got %d", name, port)
	}
	return nil
}

// Validate 校验关键配置，返回错误。
func (c *Config) Validate() error {
	if !c.Gateway.HTTP.Enabled && !c.Gateway.HTTPS.Enabled {
		return fmt.Errorf("at least one data-plane listener must be enabled")
	}
	if err := validatePort("management", c.Management.Port); err != nil {
		return err
	}
	if c.Gateway.HTTP.Enabled {
		if err := validatePort("gateway.http", c.Gateway.HTTP.Port); err != nil {
			return err
		}
	}
	if c.Gateway.HTTPS.Enabled {
		if err := validatePort("gateway.https", c.Gateway.HTTPS.Port); err != nil {
			return err
		}
		if c.Gateway.HTTPS.Cert == "" || c.Gateway.HTTPS.Key == "" {
			return fmt.Errorf("gateway.https.cert and key are required when https enabled")
		}
	}
	if c.HA.Enabled {
		if c.Database.URL == "" {
			return fmt.Errorf("ha.enabled requires database.url")
		}
	}
	switch c.TLS.Mode {
	case "", "global", "direct":
	default:
		return fmt.Errorf("tls.mode must be global or direct, got %q", c.TLS.Mode)
	}
	switch c.TLS.MinVersion {
	case "", "tls1.2", "tls1.3":
	default:
		return fmt.Errorf("tls.min_version must be tls1.2 or tls1.3, got %q", c.TLS.MinVersion)
	}
	return nil
}
