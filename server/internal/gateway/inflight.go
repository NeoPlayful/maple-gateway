package gateway

import (
	"net/http"
	"sync/atomic"
)

// inFlightLimiter 是数据面在途请求上限：超过即快速返回 503，作为过载背压。
// 目的不是提升吞吐，而是在上游变慢/连接抖动时把「堆积」变成「可观测的拒绝」，
// 避免请求在内存里无界排队把进程拖垮。limit<=0 表示不限制（直接放行）。
//
// limit 用 atomic 保存，使运行时配置变更可热更新（无需重建监听）。
type inFlightLimiter struct {
	limit   atomic.Int64
	current atomic.Int64
	next    http.Handler
}

// NewInFlightLimiter 用上限包裹下游 handler。limit<=0 时仍返回限流器（但不拦截），
// 以便始终可读在途数用于指标。
func NewInFlightLimiter(limit int, next http.Handler) *inFlightLimiter {
	l := &inFlightLimiter{next: next}
	l.limit.Store(int64(limit))
	return l
}

// SetLimit 热更新在途上限。limit<=0 表示不限。
func (l *inFlightLimiter) SetLimit(limit int) { l.limit.Store(int64(limit)) }

func (l *inFlightLimiter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 每请求读当前上限，使运行时变更即时生效。
	limit := l.limit.Load()
	// limit<=0 表示不限，直接放行（不占用计数，避免与「在途数」语义混淆）。
	if limit <= 0 {
		l.next.ServeHTTP(w, r)
		return
	}
	// 先占位再判断：原子自增后若越过上限则回退并拒绝。
	n := l.current.Add(1)
	if n > limit {
		l.current.Add(-1)
		w.Header().Set("Retry-After", "1")
		http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
		return
	}
	defer l.current.Add(-1)
	l.next.ServeHTTP(w, r)
}

// InFlight 返回当前在途请求数（供指标采集）。
func (l *inFlightLimiter) InFlight() int64 { return l.current.Load() }

// Limit 返回当前在途上限。
func (l *inFlightLimiter) Limit() int64 { return l.limit.Load() }
