// Package config 负责启动配置的加载与合并。
// 优先级：命令行默认值 < YAML 文件 < 环境变量(MAPLE_*)。
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config 是完整启动配置。
type Config struct {
	Gateway    GatewayConfig    `yaml:"gateway"`
	Management ManagementConfig `yaml:"management"`
	Health     HealthConfig     `yaml:"health"`
	Proxy      ProxyConfig      `yaml:"proxy"`
	Database   DatabaseConfig   `yaml:"database"`
	Redis      RedisConfig      `yaml:"redis"`
	Logging    LoggingConfig    `yaml:"logging"`
	Security   SecurityConfig   `yaml:"security"`
}

type GatewayConfig struct {
	HTTP  HTTPListener `yaml:"http"`
	HTTPS HTTPListener `yaml:"https"`
}

type HTTPListener struct {
	Address string `yaml:"address"`
	Enabled bool   `yaml:"enabled"`
	Cert    string `yaml:"cert"` // TLS 证书路径（Phase 2）
	Key     string `yaml:"key"`  // TLS 私钥路径（Phase 2）
}

type ManagementConfig struct {
	Address string `yaml:"address"`
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

// Default 返回内建默认配置（作为 env / 缺省兜底）。
func Default() *Config {
	return &Config{
		Gateway: GatewayConfig{
			HTTP: HTTPListener{Address: ":8000", Enabled: true},
		},
		Management: ManagementConfig{Address: ":4000"},
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
		Logging: LoggingConfig{Level: "info"},
		Security: SecurityConfig{
			AdminToken:           "",
			InstanceAllowPrivate: true,
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
	if v := os.Getenv("MAPLE_GATEWAY_HTTP_ADDR"); v != "" {
		c.Gateway.HTTP.Address = v
	}
	if v := os.Getenv("MAPLE_MANAGEMENT_ADDR"); v != "" {
		c.Management.Address = v
	}
	if v := os.Getenv("MAPLE_DATABASE_URL"); v != "" {
		c.Database.URL = v
	}
	if v := os.Getenv("MAPLE_REDIS_URL"); v != "" {
		c.Redis.URL = v
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
}

func parseBool(v string, def bool) bool {
	if b, err := strconv.ParseBool(v); err == nil {
		return b
	}
	return def
}

// Validate 校验关键配置，返回错误。
func (c *Config) Validate() error {
	if !c.Gateway.HTTP.Enabled && !c.Gateway.HTTPS.Enabled {
		return fmt.Errorf("at least one data-plane listener must be enabled")
	}
	if strings.TrimSpace(c.Management.Address) == "" {
		return fmt.Errorf("management address must not be empty")
	}
	return nil
}
