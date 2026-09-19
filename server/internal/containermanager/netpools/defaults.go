package netpools

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// 与 Gateway settings 包中 container 分区的键名保持一致的只读视图。
const (
	settingsContainerSection = "container"
	keyDefaultNetworkPool    = "default_network_pool"
	keyDefaultProjectPrefix  = "default_project_prefix"
	keyDefaultReuseEnabled   = "default_reuse_enabled"
	keyDefaultReuseDelaySec  = "default_reuse_delay_seconds"
)

// LoadSystemDefaultPool 从共享 settings 表读取系统默认网络池配置。
// 表由 Gateway 维护；CM 与 Gateway 同库，故直接只读。
// 无记录、解码失败或 DB 为空时回退内建默认（10.128.0.0/9、/24、启用复用、600s）。
func LoadSystemDefaultPool(ctx context.Context, db *sql.DB) DefaultPoolConfig {
	out := DefaultSystemPool()
	if db == nil {
		return out
	}
	rows, err := db.QueryContext(ctx, `
		SELECT key, value FROM settings WHERE section = $1`, settingsContainerSection)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var raw []byte
		if err := rows.Scan(&key, &raw); err != nil {
			continue
		}
		applyContainerKey(&out, key, raw)
	}
	if err := rows.Err(); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out
	}
	return out
}

// applyContainerKey 覆盖单个系统默认键（值非法则忽略，保留回退值）。
func applyContainerKey(out *DefaultPoolConfig, key string, raw []byte) {
	switch key {
	case keyDefaultNetworkPool:
		var s string
		if json.Unmarshal(raw, &s) == nil && s != "" {
			out.AddressPool = s
		}
	case keyDefaultProjectPrefix:
		var n int
		if json.Unmarshal(raw, &n) == nil && n > 0 {
			out.ProjectPrefix = n
		}
	case keyDefaultReuseEnabled:
		var b bool
		if json.Unmarshal(raw, &b) == nil {
			out.ReuseEnabled = b
		}
	case keyDefaultReuseDelaySec:
		var n int
		if json.Unmarshal(raw, &n) == nil && n >= 0 {
			out.ReuseDelaySeconds = n
		}
	}
}
