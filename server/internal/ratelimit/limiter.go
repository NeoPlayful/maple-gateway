package ratelimit

import (
	"sync"
	"time"
)

// Allowance 是一次判定的结果。
type Allowance struct {
	Allowed bool
	Limit   int // 命中规则的窗口配额
	Window  int // 命中规则窗口秒数
	Remain  int // 剩余可放行量
	Retry   int // 距下次可放行的秒数（上限时建议等待）
}

// Backend 是限流计数后端接口。
// *Limiter（单机内存）与 *RedisLimiter（跨实例共享）都实现它；
// 数据平面经该接口判定，便于装配时按配置选 memory / redis。
type Backend interface {
	Allow(key string, limit, windowSec, burst int, now time.Time) Allowance
}

var _ Backend = (*Limiter)(nil)

// bucket 是单 key 的滑动窗口计数状态。
type bucket struct {
	window time.Time // 当前窗口起点
	count  int       // 当前窗口已计数
	burst  int       // 突发容量（令牌桶瞬时上限）
	limit  int       // 窗口配额
	winSec int       // 窗口秒数
}

// Limiter 内存限流器：每 key 固定窗口计数 + burst 突发容忍。
// 非线程安全由内部互斥保护；周期性清理过期 key 防止内存增长。
type Limiter struct {
	mu       sync.Mutex
	buckets  map[string]*bucket
	cleanupI time.Duration
	lastGC   time.Time
}

// NewLimiter 构造。cleanup 为 key 清理周期（0 表示 60s）。
func NewLimiter(cleanup time.Duration) *Limiter {
	if cleanup <= 0 {
		cleanup = 60 * time.Second
	}
	return &Limiter{buckets: map[string]*bucket{}, cleanupI: cleanup}
}

// Allow 判定 key 是否放行：limit 次/windowSec 秒；burst>limit 时允许突发至 burst。
func (l *Limiter) Allow(key string, limit, windowSec, burst int, now time.Time) Allowance {
	if limit <= 0 {
		return Allowance{Allowed: false, Limit: limit, Window: windowSec}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.maybeGC(now)

	b, ok := l.buckets[key]
	// 配置变更（limit/window/burst）后重置窗口计数。
	if !ok || b.limit != limit || b.winSec != windowSec || b.burst != burst {
		b = &bucket{window: now, limit: limit, winSec: windowSec, burst: burst}
		l.buckets[key] = b
	}
	// 窗口过期 → 开启新窗口。
	elapsed := now.Sub(b.window)
	if elapsed >= time.Duration(b.winSec)*time.Second {
		b.window = now
		b.count = 0
	}
	// burst 突发：若 burst > limit，允许瞬间超过窗口配额到 burst 为止。
	// count 同时受两界约束：窗口配额 limit（滑动）与突发上限 burst。
	hard := burst
	if hard <= 0 || hard < limit {
		hard = limit
	}
	if b.count >= hard {
		remainWin := time.Duration(b.winSec)*time.Second - elapsed
		retry := 1
		if remainWin > 0 {
			retry = int((remainWin + time.Second - 1) / time.Second)
		}
		return Allowance{Allowed: false, Limit: limit, Window: windowSec, Remain: 0, Retry: retry}
	}
	b.count++
	remain := hard - b.count
	if remain < 0 {
		remain = 0
	}
	return Allowance{Allowed: true, Limit: limit, Window: windowSec, Remain: remain}
}

// maybeGC 定期清理超过两个窗口未访问的 key。
func (l *Limiter) maybeGC(now time.Time) {
	if now.Sub(l.lastGC) < l.cleanupI {
		return
	}
	l.lastGC = now
	threshold := now.Add(-2 * l.cleanupI)
	for k, b := range l.buckets {
		if b.window.Before(threshold) {
			delete(l.buckets, k)
		}
	}
}
