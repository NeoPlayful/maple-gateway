// Package config 负责 Node Agent 进程的启动配置加载。
// 优先级：内建默认值 < YAML 文件 < 环境变量(MAPLE_AGENT_*)。
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config 是 Node Agent 进程配置。
type Config struct {
	Agent   AgentConfig   `yaml:"agent"`
	Logging LoggingConfig `yaml:"logging"`
}

// AgentConfig 是 Agent 的业务配置。
type AgentConfig struct {
	// Listen 对外监听地址（供 CM 调用容器操作），应仅绑内网；
	// 容器内或被远程 CM 调用时需绑 0.0.0.0 并配合防火墙/allowlist。
	Listen string `yaml:"listen"`
	// Token 校验 CM 访问本 Agent 的令牌。
	Token string `yaml:"token"`
	// DockerHost Docker Engine 地址；空则用 SDK 平台默认端点：
	//   Linux  : unix:///var/run/docker.sock
	//   Windows: npipe:////./pipe/docker_engine
	// 容器内运行时通常留空并挂载 docker.sock，或显式设为 tcp://<host>:2375。
	DockerHost string `yaml:"docker_host"`
	// AllowedCIDRs 允许访问本 Agent 的来源网段；空则不限制来源（仅令牌）。
	AllowedCIDRs []string `yaml:"allowed_cidrs"`
	// AllowedImages 允许拉取的镜像前缀清单；空则不限制。
	AllowedImages []string `yaml:"allowed_images"`
	// ManagedLabel 受管容器标签键，Agent 只操作带此标签的容器。
	ManagedLabel string `yaml:"managed_label"`
	// NodeName 本节点标识（上报给 CM）。
	NodeName string `yaml:"node_name"`
}

// LoggingConfig 日志配置。
type LoggingConfig struct {
	Level  string `yaml:"level"`
	Pretty bool   `yaml:"pretty"`
}

// Default 返回内建默认配置。
func Default() *Config {
	return &Config{
		Agent: AgentConfig{
			Listen:       "127.0.0.1:9092",
			ManagedLabel: "maple.managed",
		},
		Logging: LoggingConfig{Level: "info"},
	}
}

// Load 从默认值 + 可选 YAML + 环境变量合并出最终配置。
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

// applyEnv 用 MAPLE_AGENT_* / MAPLE_LOG_* 覆盖配置项。
func (c *Config) applyEnv() {
	if v := os.Getenv("MAPLE_AGENT_LISTEN"); v != "" {
		c.Agent.Listen = v
	}
	if v := os.Getenv("MAPLE_AGENT_TOKEN"); v != "" {
		c.Agent.Token = v
	}
	if v := os.Getenv("MAPLE_AGENT_DOCKER_HOST"); v != "" {
		c.Agent.DockerHost = v
	}
	if v := os.Getenv("MAPLE_AGENT_MANAGED_LABEL"); v != "" {
		c.Agent.ManagedLabel = v
	}
	if v := os.Getenv("MAPLE_AGENT_NODE_NAME"); v != "" {
		c.Agent.NodeName = v
	}
	if v := os.Getenv("MAPLE_AGENT_ALLOWED_CIDRS"); v != "" {
		c.Agent.AllowedCIDRs = splitComma(v)
	}
	if v := os.Getenv("MAPLE_AGENT_ALLOWED_IMAGES"); v != "" {
		c.Agent.AllowedImages = splitComma(v)
	}
	if v := os.Getenv("MAPLE_LOG_LEVEL"); v != "" {
		c.Logging.Level = v
	}
	if v := os.Getenv("MAPLE_LOG_PRETTY"); v != "" {
		c.Logging.Pretty = parseBool(v, c.Logging.Pretty)
	}
}

func splitComma(s string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			if part := trim(s[start:i]); part != "" {
				out = append(out, part)
			}
			start = i + 1
		}
	}
	return out
}

func trim(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}

func parseBool(s string, def bool) bool {
	switch s {
	case "1", "true", "TRUE", "True", "yes", "on":
		return true
	case "0", "false", "FALSE", "False", "no", "off":
		return false
	default:
		return def
	}
}
