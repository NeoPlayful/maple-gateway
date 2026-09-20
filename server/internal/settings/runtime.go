package settings

import (
	"encoding/json"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/pkg"
)

// health / proxy 运行时配置键名。存于 settings 表，缺行时回退到调用方传入的
// 默认值（来自 config：内建默认 < YAML < env < DB）。
const (
	KeyHealthInterval         = "interval"
	KeyHealthTimeout          = "timeout"
	KeyHealthFailureThreshold = "failure_threshold"
	KeyHealthSuccessThreshold = "success_threshold"
	KeyHealthGracePeriod      = "grace_period"

	KeyProxyReadHeaderTimeout     = "read_header_timeout"
	KeyProxyReadTimeout           = "read_timeout"
	KeyProxyResponseHeaderTimeout = "response_header_timeout"
	KeyProxyIdleTimeout           = "idle_timeout"
	KeyProxyMaxHeaderBytes        = "max_header_bytes"
	KeyProxyMaxBodyBytes          = "max_body_bytes"
	KeyProxyMaxConnsPerHost       = "max_conns_per_host"
	KeyProxyMaxIdleConns          = "max_idle_conns"
	KeyProxyMaxIdleConnsPerHost   = "max_idle_conns_per_host"
	KeyProxyMaxInFlight           = "max_in_flight"
)

// HealthRuntime 是主动健康检查的运行时参数。时长以字符串（如 "10s"）存储。
type HealthRuntime struct {
	Interval         time.Duration `json:"-"`
	Timeout          time.Duration `json:"-"`
	FailureThreshold int           `json:"failure_threshold"`
	SuccessThreshold int           `json:"success_threshold"`
	GracePeriod      time.Duration `json:"-"`
}

// ProxyRuntime 是数据平面代理/传输的运行时参数。时长以字符串存储。
type ProxyRuntime struct {
	ReadHeaderTimeout     time.Duration `json:"-"`
	ReadTimeout           time.Duration `json:"-"`
	ResponseHeaderTimeout time.Duration `json:"-"`
	IdleTimeout           time.Duration `json:"-"`
	MaxHeaderBytes        int           `json:"max_header_bytes"`
	MaxBodyBytes          int64         `json:"max_body_bytes"`
	MaxConnsPerHost       int           `json:"max_conns_per_host"`
	MaxIdleConns          int           `json:"max_idle_conns"`
	MaxIdleConnsPerHost   int           `json:"max_idle_conns_per_host"`
	MaxInFlight           int           `json:"max_in_flight"`
}

// DefaultHealthRuntime 返回健康检查内建默认（与 config.Default 一致）。
func DefaultHealthRuntime() HealthRuntime {
	return HealthRuntime{
		Interval:         10 * time.Second,
		Timeout:          3 * time.Second,
		FailureThreshold: 3,
		SuccessThreshold: 2,
		GracePeriod:      10 * time.Second,
	}
}

// DefaultProxyRuntime 返回代理/传输内建默认（与 config.Default 一致）。
func DefaultProxyRuntime() ProxyRuntime {
	return ProxyRuntime{
		ReadHeaderTimeout:     10 * time.Second,
		ReadTimeout:           60 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
		IdleTimeout:           120 * time.Second,
		MaxHeaderBytes:        1 << 20,
		MaxBodyBytes:          10 << 20,
		MaxConnsPerHost:       512,
		MaxIdleConns:          512,
		MaxIdleConnsPerHost:   128,
		MaxInFlight:           4096,
	}
}

// normalize 用内建默认补齐非法字段，避免调用方传入残缺 def 时把网关配成不可用。
func (h HealthRuntime) normalize() HealthRuntime {
	d := DefaultHealthRuntime()
	if h.Interval <= 0 {
		h.Interval = d.Interval
	}
	if h.Timeout <= 0 {
		h.Timeout = d.Timeout
	}
	if h.FailureThreshold < 1 {
		h.FailureThreshold = d.FailureThreshold
	}
	if h.SuccessThreshold < 1 {
		h.SuccessThreshold = d.SuccessThreshold
	}
	if h.GracePeriod < 0 {
		h.GracePeriod = d.GracePeriod
	}
	return h
}

func (p ProxyRuntime) normalize() ProxyRuntime {
	d := DefaultProxyRuntime()
	if p.ReadHeaderTimeout <= 0 {
		p.ReadHeaderTimeout = d.ReadHeaderTimeout
	}
	if p.ReadTimeout <= 0 {
		p.ReadTimeout = d.ReadTimeout
	}
	if p.ResponseHeaderTimeout <= 0 {
		p.ResponseHeaderTimeout = d.ResponseHeaderTimeout
	}
	if p.IdleTimeout <= 0 {
		p.IdleTimeout = d.IdleTimeout
	}
	if p.MaxHeaderBytes <= 0 {
		p.MaxHeaderBytes = d.MaxHeaderBytes
	}
	// max_body_bytes / max_in_flight：0 表示不限，是合法值，不补齐。
	if p.MaxBodyBytes < 0 {
		p.MaxBodyBytes = d.MaxBodyBytes
	}
	if p.MaxConnsPerHost < 0 {
		p.MaxConnsPerHost = d.MaxConnsPerHost
	}
	if p.MaxIdleConns < 0 {
		p.MaxIdleConns = d.MaxIdleConns
	}
	if p.MaxIdleConnsPerHost < 0 {
		p.MaxIdleConnsPerHost = d.MaxIdleConnsPerHost
	}
	if p.MaxInFlight < 0 {
		p.MaxInFlight = d.MaxInFlight
	}
	return p
}

// getDuration 读一个时长键（存为 "10s" 字符串）。缺行、非法或小于 min 时回退 def。
func (r *Repository) getDuration(section Section, key string, def time.Duration, min time.Duration) time.Duration {
	e, ok := r.Get(section, key)
	if !ok {
		return def
	}
	var s string
	if json.Unmarshal(e.Value, &s) != nil {
		return def
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < min {
		return def
	}
	return d
}

// getInt 读一个整型键。缺行、非法或小于 min 时回退 def。
func (r *Repository) getInt(section Section, key string, def, min int) int {
	e, ok := r.Get(section, key)
	if !ok {
		return def
	}
	var n int
	if json.Unmarshal(e.Value, &n) != nil || n < min {
		return def
	}
	return n
}

// getInt64 读一个 int64 键（如 max_body_bytes）。缺行、非法或小于 min 时回退 def。
func (r *Repository) getInt64(section Section, key string, def, min int64) int64 {
	e, ok := r.Get(section, key)
	if !ok {
		return def
	}
	var n int64
	if json.Unmarshal(e.Value, &n) != nil || n < min {
		return def
	}
	return n
}

// Health 读取健康检查运行时配置（def 为 config 提供的默认，逐键被 DB 覆盖）。
func (r *Repository) Health(def HealthRuntime) HealthRuntime {
	def = def.normalize()
	return HealthRuntime{
		Interval:         r.getDuration(SectionHealth, KeyHealthInterval, def.Interval, time.Nanosecond),
		Timeout:          r.getDuration(SectionHealth, KeyHealthTimeout, def.Timeout, time.Nanosecond),
		FailureThreshold: r.getInt(SectionHealth, KeyHealthFailureThreshold, def.FailureThreshold, 1),
		SuccessThreshold: r.getInt(SectionHealth, KeyHealthSuccessThreshold, def.SuccessThreshold, 1),
		GracePeriod:      r.getDuration(SectionHealth, KeyHealthGracePeriod, def.GracePeriod, 0),
	}
}

// Proxy 读取代理/传输运行时配置（def 为 config 提供的默认，逐键被 DB 覆盖）。
func (r *Repository) Proxy(def ProxyRuntime) ProxyRuntime {
	def = def.normalize()
	return ProxyRuntime{
		ReadHeaderTimeout:     r.getDuration(SectionProxy, KeyProxyReadHeaderTimeout, def.ReadHeaderTimeout, time.Nanosecond),
		ReadTimeout:           r.getDuration(SectionProxy, KeyProxyReadTimeout, def.ReadTimeout, time.Nanosecond),
		ResponseHeaderTimeout: r.getDuration(SectionProxy, KeyProxyResponseHeaderTimeout, def.ResponseHeaderTimeout, time.Nanosecond),
		IdleTimeout:           r.getDuration(SectionProxy, KeyProxyIdleTimeout, def.IdleTimeout, time.Nanosecond),
		MaxHeaderBytes:        r.getInt(SectionProxy, KeyProxyMaxHeaderBytes, def.MaxHeaderBytes, 1),
		MaxBodyBytes:          r.getInt64(SectionProxy, KeyProxyMaxBodyBytes, def.MaxBodyBytes, 0),
		MaxConnsPerHost:       r.getInt(SectionProxy, KeyProxyMaxConnsPerHost, def.MaxConnsPerHost, 0),
		MaxIdleConns:          r.getInt(SectionProxy, KeyProxyMaxIdleConns, def.MaxIdleConns, 0),
		MaxIdleConnsPerHost:   r.getInt(SectionProxy, KeyProxyMaxIdleConnsPerHost, def.MaxIdleConnsPerHost, 0),
		MaxInFlight:           r.getInt(SectionProxy, KeyProxyMaxInFlight, def.MaxInFlight, 0),
	}
}

// ValidateRuntimeKey 校验 health/proxy 运行时键的取值；未知键或非本分区返回 nil。
// 供写入前拦截非法值，避免把网关配成不可用。
func ValidateRuntimeKey(section Section, key string, raw json.RawMessage) error {
	switch section {
	case SectionHealth:
		switch key {
		case KeyHealthInterval, KeyHealthTimeout:
			return validateDuration(raw, key, time.Nanosecond)
		case KeyHealthGracePeriod:
			return validateDuration(raw, key, 0)
		case KeyHealthFailureThreshold, KeyHealthSuccessThreshold:
			return validateInt(raw, key, 1)
		}
	case SectionProxy:
		switch key {
		case KeyProxyReadHeaderTimeout, KeyProxyReadTimeout,
			KeyProxyResponseHeaderTimeout, KeyProxyIdleTimeout:
			return validateDuration(raw, key, time.Nanosecond)
		case KeyProxyMaxHeaderBytes:
			return validateInt(raw, key, 1)
		case KeyProxyMaxBodyBytes:
			return validateInt64(raw, key, 0)
		case KeyProxyMaxConnsPerHost, KeyProxyMaxIdleConns,
			KeyProxyMaxIdleConnsPerHost, KeyProxyMaxInFlight:
			return validateInt(raw, key, 0)
		}
	}
	return nil
}

func validateDuration(raw json.RawMessage, key string, min time.Duration) error {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return errInvalidValue(key, "须为时长字符串，如 \"10s\"")
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < min {
		return errInvalidValue(key, "时长非法或小于下限")
	}
	return nil
}

func validateInt(raw json.RawMessage, key string, min int) error {
	var n int
	if json.Unmarshal(raw, &n) != nil || n < min {
		return errInvalidValue(key, "整数非法或小于下限")
	}
	return nil
}

func validateInt64(raw json.RawMessage, key string, min int64) error {
	var n int64
	if json.Unmarshal(raw, &n) != nil || n < min {
		return errInvalidValue(key, "整数非法或小于下限")
	}
	return nil
}

func errInvalidValue(key, why string) error {
	return pkg.ErrValidation("配置 " + key + " " + why)
}
