// Package logs 提供访问/路由日志的环形缓冲与 Management API 查询。
//
// 数据平面（proxy）把每个转发请求的访问记录写入内存环形缓冲（AccessLog）；
// 管理端经 /api/admin/logs/access 分类查询（按时间/状态/host 过滤，分页）。
// 不引入独立日志服务；错误/审计类已分别落 DB(audit_logs) 与 zap。
package logs

import (
	"sync"
	"time"
)

// AccessEntry 是一次数据平面请求的访问记录。
type AccessEntry struct {
	Timestamp  time.Time `json:"timestamp"`
	Host       string    `json:"host"`
	Method     string    `json:"method"`
	Path       string    `json:"path"`
	Status     int       `json:"status"`
	ClientIP   string    `json:"client_ip"`
	RequestID  string    `json:"request_id,omitempty"` // 关联 OTel trace / 跨组件排查
	DurationMS int64     `json:"duration_ms"`
}

// AccessLog 是访问日志环形缓冲（线程安全，固定容量）。
type AccessLog struct {
	mu   sync.RWMutex
	ring []AccessEntry
	head int // 下一写入位
	full bool
}

// NewAccessLog 构造。cap 为保留的最大条数。
func NewAccessLog(cap int) *AccessLog {
	if cap <= 0 {
		cap = 5000
	}
	return &AccessLog{ring: make([]AccessEntry, cap)}
}

// Append 写入一条访问记录。
func (a *AccessLog) Append(e AccessEntry) {
	a.mu.Lock()
	a.ring[a.head] = e
	a.head = (a.head + 1) % len(a.ring)
	if a.head == 0 {
		a.full = true
	}
	a.mu.Unlock()
}

// Query 按时间倒序返回过滤后的记录，支持 request_id / host / status / limit/offset。
func (a *AccessLog) Query(host string, status int, requestID string, from, to time.Time, limit, offset int) []AccessEntry {
	a.mu.RLock()
	defer a.mu.RUnlock()
	// 从最新往旧遍历（head-1 是最新写入）。
	n := a.head
	if a.full {
		n = len(a.ring)
	}
	out := []AccessEntry{}
	skipped := 0
	for i := 0; i < n; i++ {
		idx := (a.head - 1 - i + len(a.ring)) % len(a.ring)
		e := a.ring[idx]
		if e.Timestamp.IsZero() {
			continue // 未写位置
		}
		if requestID != "" && e.RequestID != requestID {
			continue
		}
		if host != "" && e.Host != host {
			continue
		}
		if status != 0 && e.Status != status {
			continue
		}
		if !from.IsZero() && e.Timestamp.Before(from) {
			continue
		}
		if !to.IsZero() && e.Timestamp.After(to) {
			continue
		}
		if skipped < offset {
			skipped++
			continue
		}
		if len(out) >= limit {
			break
		}
		out = append(out, e)
	}
	return out
}

// Count 返回当前已写条数（诊断）。
func (a *AccessLog) Count() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.full {
		return len(a.ring)
	}
	return a.head
}

// ErrEntry 是数据平面错误日志记录（upstream / 路由拒绝）。
type ErrEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Host      string    `json:"host"`
	Path      string    `json:"path"`
	Status    int       `json:"status"`
	RequestID string    `json:"request_id,omitempty"` // 关联 OTel trace / 跨组件排查
	Error     string    `json:"error"`
}

// ErrLog 是错误日志环形缓冲（线程安全，固定容量）。
type ErrLog struct {
	mu   sync.RWMutex
	ring []ErrEntry
	head int
	full bool
}

// NewErrLog 构造。cap 为保留的最大条数。
func NewErrLog(cap int) *ErrLog {
	if cap <= 0 {
		cap = 5000
	}
	return &ErrLog{ring: make([]ErrEntry, cap)}
}

// Append 写入一条错误记录。
func (e *ErrLog) Append(entry ErrEntry) {
	e.mu.Lock()
	e.ring[e.head] = entry
	e.head = (e.head + 1) % len(e.ring)
	if e.head == 0 {
		e.full = true
	}
	e.mu.Unlock()
}

// Query 按时间倒序返回过滤后的错误记录（request_id / host / status / from / to + limit/offset）。
func (e *ErrLog) Query(host string, status int, requestID string, from, to time.Time, limit, offset int) []ErrEntry {
	e.mu.RLock()
	defer e.mu.RUnlock()
	n := e.head
	if e.full {
		n = len(e.ring)
	}
	out := []ErrEntry{}
	skipped := 0
	for i := 0; i < n; i++ {
		idx := (e.head - 1 - i + len(e.ring)) % len(e.ring)
		entry := e.ring[idx]
		if entry.Timestamp.IsZero() {
			continue
		}
		if requestID != "" && entry.RequestID != requestID {
			continue
		}
		if host != "" && entry.Host != host {
			continue
		}
		if status != 0 && entry.Status != status {
			continue
		}
		if !from.IsZero() && entry.Timestamp.Before(from) {
			continue
		}
		if !to.IsZero() && entry.Timestamp.After(to) {
			continue
		}
		if skipped < offset {
			skipped++
			continue
		}
		if len(out) >= limit {
			break
		}
		out = append(out, entry)
	}
	return out
}

// Count 返回当前已写条数。
func (e *ErrLog) Count() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.full {
		return len(e.ring)
	}
	return e.head
}
