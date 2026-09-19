package settings

import (
	"context"
	"encoding/json"
	"fmt"
)

// container section 下的键名。系统默认只作用于未来新节点的默认池，
// 不回溯修改任何已存在的池（文档第13节 / 第44节 / 第97节）。
const (
	KeyDefaultNetworkPool       = "default_network_pool"
	KeyDefaultProjectPrefix     = "default_project_prefix"
	KeyAllocationMode           = "allocation_mode"
	KeyDefaultReuseEnabled      = "default_reuse_enabled"
	KeyDefaultReuseDelaySeconds = "default_reuse_delay_seconds"
)

// 系统默认值（无记录时的回退，与内建默认一致）。
const (
	DefaultNetworkPool       = "10.128.0.0/9"
	DefaultProjectPrefix     = 24
	DefaultAllocationMode    = "sequential"
	DefaultReuseEnabled      = true
	DefaultReuseDelaySeconds = 600
)

// ContainerNetworkConfig 是系统级容器网络默认配置。
type ContainerNetworkConfig struct {
	DefaultNetworkPool       string `json:"default_network_pool"`
	DefaultProjectPrefix     int    `json:"default_project_prefix"`
	AllocationMode           string `json:"allocation_mode"`
	DefaultReuseEnabled      bool   `json:"default_reuse_enabled"`
	DefaultReuseDelaySeconds int    `json:"default_reuse_delay_seconds"`
}

// DefaultContainerNetwork 返回系统默认容器网络配置。
func DefaultContainerNetwork() ContainerNetworkConfig {
	return ContainerNetworkConfig{
		DefaultNetworkPool:       DefaultNetworkPool,
		DefaultProjectPrefix:     DefaultProjectPrefix,
		AllocationMode:           DefaultAllocationMode,
		DefaultReuseEnabled:      DefaultReuseEnabled,
		DefaultReuseDelaySeconds: DefaultReuseDelaySeconds,
	}
}

// GetContainerNetwork 读取系统默认容器网络配置（缺失键回退内建默认）。
func (r *Repository) GetContainerNetwork() ContainerNetworkConfig {
	out := DefaultContainerNetwork()
	if e, ok := r.Get(SectionContainer, KeyDefaultNetworkPool); ok {
		var s string
		if json.Unmarshal(e.Value, &s) == nil && s != "" {
			out.DefaultNetworkPool = s
		}
	}
	if e, ok := r.Get(SectionContainer, KeyDefaultProjectPrefix); ok {
		var n int
		if json.Unmarshal(e.Value, &n) == nil && n > 0 {
			out.DefaultProjectPrefix = n
		}
	}
	if e, ok := r.Get(SectionContainer, KeyAllocationMode); ok {
		var s string
		if json.Unmarshal(e.Value, &s) == nil && s != "" {
			out.AllocationMode = s
		}
	}
	if e, ok := r.Get(SectionContainer, KeyDefaultReuseEnabled); ok {
		var b bool
		if json.Unmarshal(e.Value, &b) == nil {
			out.DefaultReuseEnabled = b
		}
	}
	if e, ok := r.Get(SectionContainer, KeyDefaultReuseDelaySeconds); ok {
		var n int
		if json.Unmarshal(e.Value, &n) == nil && n >= 0 {
			out.DefaultReuseDelaySeconds = n
		}
	}
	return out
}

// SaveContainerNetwork 逐键写入系统默认容器网络配置。
func (r *Repository) SaveContainerNetwork(ctx context.Context, cfg ContainerNetworkConfig) error {
	items := map[string]any{
		KeyDefaultNetworkPool:       cfg.DefaultNetworkPool,
		KeyDefaultProjectPrefix:     cfg.DefaultProjectPrefix,
		KeyAllocationMode:           cfg.AllocationMode,
		KeyDefaultReuseEnabled:      cfg.DefaultReuseEnabled,
		KeyDefaultReuseDelaySeconds: cfg.DefaultReuseDelaySeconds,
	}
	for key, val := range items {
		raw, err := json.Marshal(val)
		if err != nil {
			return fmt.Errorf("marshal container setting %s: %w", key, err)
		}
		if _, err := r.Upsert(ctx, SectionContainer, key, raw); err != nil {
			return err
		}
	}
	return nil
}
