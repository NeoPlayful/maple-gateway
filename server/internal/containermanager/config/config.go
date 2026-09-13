// Package config 负责 Container Manager 进程的启动配置加载。
// 优先级：内建默认值 < YAML 文件 < 环境变量(MAPLE_CM_*)。
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

// Config 是 Container Manager 进程配置。
type Config struct {
	CM      CMConfig      `yaml:"cm"`
	Logging LoggingConfig `yaml:"logging"`
}

// CMConfig 是 CM 的业务配置。
type CMConfig struct {
	// Listen 对外监听地址（接收 Gateway 下发的部署意图、状态查询）。
	Listen string `yaml:"listen"`
	// Token 校验 Gateway 访问本进程的令牌。
	Token string `yaml:"token"`
	// GatewayBaseURL 状态上行目标（Gateway 内部 API 基址）。
	GatewayBaseURL string `yaml:"gateway_base_url"`
	// GatewayToken 上报 Gateway 所用的令牌（= Gateway 的 MAPLE_INTERNAL_TOKEN）。
	GatewayToken string `yaml:"gateway_token"`
	// ReconcileInterval 期望态对账周期。
	ReconcileInterval time.Duration `yaml:"reconcile_interval"`
	// ObserveInterval 采集各节点容器/资源状态的周期。
	ObserveInterval time.Duration `yaml:"observe_interval"`
	// DefaultReplicas 期望态未指定副本数时的缺省值。
	DefaultReplicas int `yaml:"default_replicas"`
	// AgentListen 接收 Agent 反连的 WebSocket 监听地址（独立于面向 Gateway 的端口）。
	AgentListen string `yaml:"agent_listen"`
	// EnrollmentRequired 首注册是否强制校验 Enrollment Token（生产应为 true）。
	EnrollmentRequired bool `yaml:"enrollment_required"`
	// TaskTimeout 单条下发任务的执行时限。
	TaskTimeout time.Duration `yaml:"task_timeout"`
	// DatabaseURL PostgreSQL 连接串（与 Gateway 同库）。空则回退内存模式，
	// 节点/令牌/任务/期望态不持久化（仅开发用）。
	DatabaseURL string `yaml:"database_url"`
	// Nodes 静态登记的节点清单：CM 据此探测 Agent 并采集容器状态。
	// 生产可由 Agent 自注册扩展；本期以配置登记为主。
	Nodes []NodeConfig `yaml:"nodes"`
}

// NodeConfig 是一个节点的静态登记项。
// 仅提供节点名与调度属性；节点身份（Gateway UUID）由 Agent 反连时按名解析。
type NodeConfig struct {
	// Name 节点名（须与对应 Agent 的 node_name 一致；也是 Gateway nodes.name）。
	Name string `yaml:"name"`
	// Host 节点地址（管理面，上报 Gateway 供展示）。
	Host string `yaml:"host"`
	// Region 区域（调度亲和）。
	Region string `yaml:"region"`
	// Labels 节点标签（调度约束 node_selector 匹配）。
	Labels map[string]string `yaml:"labels"`
}

// LoggingConfig 日志配置。
type LoggingConfig struct {
	Level  string `yaml:"level"`
	Pretty bool   `yaml:"pretty"`
}

// Default 返回内建默认配置。
func Default() *Config {
	return &Config{
		CM: CMConfig{
			Listen:             ":9091",
			AgentListen:        ":9093",
			ReconcileInterval:  5 * time.Second,
			ObserveInterval:    10 * time.Second,
			DefaultReplicas:    1,
			TaskTimeout:        5 * time.Minute,
			EnrollmentRequired: false,
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

// applyEnv 用 MAPLE_CM_* / MAPLE_LOG_* 覆盖配置项。
func (c *Config) applyEnv() {
	if v := os.Getenv("MAPLE_CM_LISTEN"); v != "" {
		c.CM.Listen = v
	}
	if v := os.Getenv("MAPLE_CM_TOKEN"); v != "" {
		c.CM.Token = v
	}
	if v := os.Getenv("MAPLE_CM_GATEWAY_BASE_URL"); v != "" {
		c.CM.GatewayBaseURL = v
	}
	if v := os.Getenv("MAPLE_CM_GATEWAY_TOKEN"); v != "" {
		c.CM.GatewayToken = v
	}
	if v := os.Getenv("MAPLE_CM_RECONCILE_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			c.CM.ReconcileInterval = d
		}
	}
	if v := os.Getenv("MAPLE_CM_OBSERVE_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			c.CM.ObserveInterval = d
		}
	}
	if v := os.Getenv("MAPLE_CM_DEFAULT_REPLICAS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.CM.DefaultReplicas = n
		}
	}
	if v := os.Getenv("MAPLE_CM_AGENT_LISTEN"); v != "" {
		c.CM.AgentListen = v
	}
	if v := os.Getenv("MAPLE_CM_ENROLLMENT_REQUIRED"); v != "" {
		c.CM.EnrollmentRequired = parseBool(v, c.CM.EnrollmentRequired)
	}
	if v := os.Getenv("MAPLE_CM_TASK_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			c.CM.TaskTimeout = d
		}
	}
	if v := os.Getenv("MAPLE_CM_DATABASE_URL"); v != "" {
		c.CM.DatabaseURL = v
	} else if v := os.Getenv("MAPLE_DATABASE_URL"); v != "" {
		c.CM.DatabaseURL = v
	}
	if v := os.Getenv("MAPLE_LOG_LEVEL"); v != "" {
		c.Logging.Level = v
	}
	if v := os.Getenv("MAPLE_LOG_PRETTY"); v != "" {
		c.Logging.Pretty = parseBool(v, c.Logging.Pretty)
	}
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
