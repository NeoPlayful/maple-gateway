package gateway

import (
	"net/http"
	"sync/atomic"
)

// bodyLimiter 按当前上限限制请求体大小，超限时读取报错（由上游按 413/400 处理）。
// limit 用 atomic 保存以便运行时热更新；limit<=0 表示不限。
type bodyLimiter struct {
	limit   atomic.Int64
	next    http.Handler
	onLimit func(w http.ResponseWriter)
}

// NewBodyLimiter 用上限包裹下游 handler。limit<=0 时不限制（仍返回包装器便于热更）。
func NewBodyLimiter(limit int64, next http.Handler) *bodyLimiter {
	l := &bodyLimiter{next: next}
	l.limit.Store(limit)
	return l
}

// SetLimit 热更新请求体上限。limit<=0 表示不限。
func (l *bodyLimiter) SetLimit(limit int64) { l.limit.Store(limit) }

func (l *bodyLimiter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	limit := l.limit.Load()
	if limit <= 0 || r.Body == nil {
		l.next.ServeHTTP(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	l.next.ServeHTTP(w, r)
}
