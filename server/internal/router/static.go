package router

import (
	"context"

	"gopkg.in/yaml.v3"
)

// StaticConfig 是静态路由配置（S2 数据平面验证用，最终配置存 PostgreSQL）。
type StaticConfig struct {
	// Entries 按 hostname 记录转发目标与可选状态。
	Entries map[string]StaticEntry `yaml:"entries"`
}

// StaticEntry 描述单个 Host 的转发规则。
type StaticEntry struct {
	Scheme  string `yaml:"scheme"`  // http / https，默认 http
	Address string `yaml:"address"` // 形如 host:port
	Status  string `yaml:"status"`  // active / disabled；留空视为 active
}

// StaticResolver 是 S2 阶段的内存静态路由表。
type StaticResolver struct {
	entries map[string]StaticEntry
}

// NewStaticResolver 从 YAML 字节构建静态 resolver。
func NewStaticResolver(raw []byte) (*StaticResolver, error) {
	var cfg StaticConfig
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	if cfg.Entries == nil {
		cfg.Entries = map[string]StaticEntry{}
	}
	r := &StaticResolver{entries: cfg.Entries}
	return r, nil
}

// FromMap 从 map 构建（便于测试 / 内存注入）。
func FromMap(entries map[string]StaticEntry) *StaticResolver {
	if entries == nil {
		entries = map[string]StaticEntry{}
	}
	return &StaticResolver{entries: entries}
}

// Resolve 实现 Resolver 接口。
func (r *StaticResolver) Resolve(_ context.Context, host string) (*Target, error) {
	norm, err := NormalizeHost(host)
	if err != nil {
		return nil, ErrNotFound
	}
	e, ok := r.entries[norm]
	if !ok {
		return nil, ErrNotFound
	}
	if e.Status == "disabled" {
		return nil, ErrDomainDisabled
	}
	scheme := e.Scheme
	if scheme == "" {
		scheme = "http"
	}
	return &Target{Scheme: scheme, Host: e.Address}, nil
}
