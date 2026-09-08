// Package settings 提供动态配置（Settings）。
//
// 配置按 section(key 组) 持久化到 settings 表，管理端 GET/PATCH 读写；
// 每次写入 version +1 用于审计与回滚（配合 audit_logs 记录 action=settings）。
// Store 缓存全量到内存，PATCH 后 Reload；消费方（health/proxy）每周期读 Store 即热生效。
package settings

import (
	"encoding/json"
	"time"
)

// Section 是配置分组。
type Section string

// 预定义 section（对应 plan 11.3 端点）。
const (
	SectionGateway  Section = "gateway"
	SectionProxy    Section = "proxy"
	SectionHealth   Section = "health"
	SectionSecurity Section = "security"
	SectionLogging  Section = "logging"
	SectionMetrics  Section = "metrics"
)

// Entry 是一条配置键值。
type Entry struct {
	Section   Section         `json:"section"`
	Key       string          `json:"key"`
	Value     json.RawMessage `json:"value"`
	Version   int             `json:"version"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// HistoryEntry 是 settings_history 的一条历史版本。
type HistoryEntry struct {
	Version   int             `json:"version"`
	Value     json.RawMessage `json:"value"`
	ChangedAt time.Time       `json:"changed_at"`
}
