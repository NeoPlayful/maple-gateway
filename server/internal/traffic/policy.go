// Package traffic 提供数据平面流量策略的模型与匹配逻辑。
//
// 策略是版本分流的入口：请求先按 priority 依次匹配策略（header / cookie /
// path / percent），命中则定向到 target version；无命中或无策略时退化为
// 版本权重分配（或 Phase 1 的单一实例池）。
package traffic

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/internal/router"
	"github.com/google/uuid"
)

// Status 是策略状态。
type Status string

const (
	StatusEnabled  Status = "enabled"
	StatusDisabled Status = "disabled"
)

// Match 描述一条策略的匹配规则（对应 traffic_policies.match JSONB）。
// 多个条件同时满足才命中（AND 语义）；percent 单独成类，不入 AND。
type Match struct {
	Header  map[string]string `json:"header,omitempty"`  // header 精确匹配
	Cookie  map[string]string `json:"cookie,omitempty"`  // cookie 精确匹配
	Path    string            `json:"path,omitempty"`    // path 前缀匹配
	PathExact string          `json:"path_exact,omitempty"` // path 精确匹配
	Percent int               `json:"percent,omitempty"` // 0-100 流量百分比（此策略独享该比例）
}

// Sticky 是会话保持配置。
type Sticky struct {
	// HeaderName 指定用哪个请求头作为会话键（如 X-User-Id）；优先于 cookie。
	HeaderName string `json:"header_name,omitempty"`
	// CookieName 指定用哪个 cookie 作为会话键；缺省 MAPLE_SRV。
	CookieName string `json:"cookie_name,omitempty"`
	TTLSeconds int    `json:"ttl_seconds,omitempty"`
}

// Policy 是一条编译后的流量策略。
type Policy struct {
	ID              uuid.UUID  `json:"id"`
	ServiceID       uuid.UUID  `json:"service_id"`
	Name            string     `json:"name"`
	Priority        int        `json:"priority"`
	Match           Match      `json:"match"`
	TargetVersionID *uuid.UUID `json:"target_version_id,omitempty"`
	Weight          int        `json:"weight"` // 未指定目标版本时按权重参与版本分流
	Sticky          *Sticky    `json:"sticky,omitempty"`
	Status          Status     `json:"status"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// PolicyRow 是 repository 行的解耦形态（match 为 JSONB 文本）。
type PolicyRow struct {
	ID              uuid.UUID
	ServiceID       uuid.UUID
	Name            string
	Priority        int
	Match           json.RawMessage
	TargetVersionID *uuid.UUID
	Weight          int
	Sticky          json.RawMessage
	Status          Status
}

// NewMatch 从 JSONB 文本解析匹配规则；非法返回错误。
func NewMatch(raw json.RawMessage) (Match, error) {
	var m Match
	if len(raw) == 0 {
		return m, nil
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return Match{}, fmt.Errorf("parse match: %w", err)
	}
	if m.Percent < 0 || m.Percent > 100 {
		return Match{}, errors.New("percent must be 0-100")
	}
	return m, nil
}

// NewSticky 从 JSONB 文本解析 sticky。
func NewSticky(raw json.RawMessage) (*Sticky, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var s Sticky
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("parse sticky: %w", err)
	}
	return &s, nil
}

// RequestView 是数据平面请求中策略匹配所需的子集（由 router.MatchView 转换，见 ViewFrom）。
type RequestView struct {
	Header   http.Header
	Path     string
	ClientIP string // 客户端 IP（限流 ip scope 用）
}

// ViewFrom 从 router.MatchView 构造匹配上下文。
func ViewFrom(mv router.MatchView) RequestView {
	h := make(http.Header, len(mv.Header))
	for k, v := range mv.Header {
		h.Set(k, v)
	}
	return RequestView{Header: h, Path: mv.Path, ClientIP: mv.ClientIP}
}

// Matches 判断该策略是否命中请求。
func (p *Policy) Matches(v RequestView) bool {
	if p.Status != StatusEnabled {
		return false
	}
	m := p.Match
	for k, want := range m.Header {
		got := v.Header.Get(k)
		if got != want {
			return false
		}
	}
	for k, want := range m.Cookie {
		got := cookieValue(v.Header.Get("Cookie"), k)
		if got != want {
			return false
		}
	}
	if m.PathExact != "" && v.Path != m.PathExact {
		return false
	}
	if m.Path != "" && !strings.HasPrefix(v.Path, m.Path) {
		return false
	}
	return true
}

// cookieValue 从 Cookie 头取单个 cookie 值（分号分隔，trim）。
func cookieValue(raw, name string) string {
	for _, part := range strings.Split(raw, ";") {
		part = strings.TrimSpace(part)
		if kv := strings.SplitN(part, "=", 2); len(kv) == 2 && kv[0] == name {
			return kv[1]
		}
	}
	return ""
}
