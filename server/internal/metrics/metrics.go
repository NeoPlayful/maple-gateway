// Package metrics 提供轻量自研指标采集与 Prometheus 文本输出。
//
// 数据平面（proxy/cache/ratelimit）调用计数与耗时上报；/metrics 端点
// 以 Prometheus 文本格式导出。单进程内存原子计数，无需外部依赖。
package metrics

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// atomicAddFloat / atomicLoadFloat：旧 Go 版本 sync/atomic 无浮点类型化方法，
// 用 math.Float64bits 位运算实现原子读写。
func atomicAddFloat(addr *int64, delta float64) {
	for {
		old := atomic.LoadInt64(addr)
		cur := math.Float64frombits(uint64(old)) + delta
		if atomic.CompareAndSwapInt64(addr, old, int64(math.Float64bits(cur))) {
			return
		}
	}
}

func atomicLoadFloat(addr *int64) float64 {
	return math.Float64frombits(uint64(atomic.LoadInt64(addr)))
}

// bucket 上限（秒）。histogram 落桶计数，桶边界见 durationBuckets。
var durationBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5}

// sample 是带标签集的计数样本。
type sample struct {
	labels string // 已编码 "k=v,k=v"
	value  int64
}

// histogram 是带标签集的耗时分布。
type histo struct {
	labels string
	buck   []int64 // 与 durationBuckets 对齐；多一个 +Inf
	sum    int64   // float64 位模式，atomicAddFloat 维护
	count  int64
}

// Registry 指标注册表。
type Registry struct {
	mu       sync.RWMutex
	counters map[string]*counterSet
	gauges   map[string]*gaugeSet
	histos   map[string]*histoSet
}

type counterSet struct{ m map[string]*int64 }
type gaugeSet struct{ m map[string]*int64 }
type histoSet struct{ m map[string]*histo }

// NewRegistry 构造。
func NewRegistry() *Registry {
	return &Registry{
		counters: map[string]*counterSet{},
		gauges:   map[string]*gaugeSet{},
		histos:   map[string]*histoSet{},
	}
}

func encLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for i, k := range keys {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(k)
		sb.WriteByte('=')
		sb.WriteString(labels[k])
	}
	return sb.String()
}

// Counter 递增/递减。labels 为维度（tenant/service/status 等）。
func (r *Registry) Add(name string, delta int64, labels map[string]string) {
	key := encLabels(labels)
	r.mu.Lock()
	cs, ok := r.counters[name]
	if !ok {
		cs = &counterSet{m: map[string]*int64{}}
		r.counters[name] = cs
	}
	v, ok := cs.m[key]
	if !ok {
		v = new(int64)
		cs.m[key] = v
	}
	r.mu.Unlock()
	atomic.AddInt64(v, delta)
}

// Inc 计数 +1。
func (r *Registry) Inc(name string, labels map[string]string) { r.Add(name, 1, labels) }

// SetGauge 设置 gauge 值。
func (r *Registry) SetGauge(name string, value int64, labels map[string]string) {
	key := encLabels(labels)
	r.mu.Lock()
	gs, ok := r.gauges[name]
	if !ok {
		gs = &gaugeSet{m: map[string]*int64{}}
		r.gauges[name] = gs
	}
	v, ok := gs.m[key]
	if !ok {
		v = new(int64)
		gs.m[key] = v
	}
	r.mu.Unlock()
	atomic.StoreInt64(v, value)
}

// ObserveDuration 记录一次耗时（duration 秒）。
func (r *Registry) ObserveDuration(name string, d time.Duration, labels map[string]string) {
	key := encLabels(labels)
	r.mu.Lock()
	hs, ok := r.histos[name]
	if !ok {
		hs = &histoSet{m: map[string]*histo{}}
		r.histos[name] = hs
	}
	h, ok := hs.m[key]
	if !ok {
		h = &histo{buck: make([]int64, len(durationBuckets)+1)}
		hs.m[key] = h
	}
	r.mu.Unlock()

	sec := d.Seconds()
	idx := len(durationBuckets) // +Inf
	for i, b := range durationBuckets {
		if sec <= b {
			idx = i
			break
		}
	}
	atomic.AddInt64(&h.buck[idx], 1)
	atomic.AddInt64(&h.count, 1)
	atomicAddFloat(&h.sum, d.Seconds())
}

// metricLine 生成 Prometheus 文本。
func (r *Registry) Render(buf *strings.Builder) {
	r.mu.RLock()
	names := make([]string, 0, len(r.counters)+len(r.gauges)+len(r.histos))
	for n := range r.counters {
		names = append(names, n)
	}
	for n := range r.gauges {
		names = append(names, n)
	}
	for n := range r.histos {
		names = append(names, n)
	}
	sort.Strings(names)
	_ = names

	writeCounter := func(name string, cs *counterSet, typ string) {
		keys := make([]string, 0, len(cs.m))
		for k := range cs.m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		fmt.Fprintf(buf, "# TYPE %s %s\n", name, typ)
		for _, k := range keys {
			v := atomic.LoadInt64(cs.m[k])
			fmt.Fprintf(buf, "%s{%s} %d\n", name, k, v)
		}
	}
	writeGauge := func(name string, gs *gaugeSet) {
		keys := make([]string, 0, len(gs.m))
		for k := range gs.m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		fmt.Fprintf(buf, "# TYPE %s gauge\n", name)
		for _, k := range keys {
			v := atomic.LoadInt64(gs.m[k])
			fmt.Fprintf(buf, "%s{%s} %d\n", name, k, v)
		}
	}
	for _, name := range names {
		if cs, ok := r.counters[name]; ok {
			writeCounter(name, cs, "counter")
			continue
		}
		if gs, ok := r.gauges[name]; ok {
			writeGauge(name, gs)
			continue
		}
	}
	// histogram 单独输出（含 sum/count）。
	hnames := make([]string, 0, len(r.histos))
	for n := range r.histos {
		hnames = append(hnames, n)
	}
	sort.Strings(hnames)
	for _, name := range hnames {
		hs := r.histos[name]
		keys := make([]string, 0, len(hs.m))
		for k := range hs.m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		fmt.Fprintf(buf, "# TYPE %s histogram\n", name)
		for _, k := range keys {
			h := hs.m[k]
			cum := int64(0)
			for i := range h.buck {
				cum += atomic.LoadInt64(&h.buck[i])
				if i < len(durationBuckets) {
					fmt.Fprintf(buf, "%s_bucket{%s,le=%q} %d\n", name, k,
						fmt.Sprintf("%g", durationBuckets[i]), cum)
				}
			}
			fmt.Fprintf(buf, "%s_bucket{%s,le=\"+Inf\"} %d\n", name, k, cum)
			fmt.Fprintf(buf, "%s_sum{%s} %g\n", name, k, atomicLoadFloat(&h.sum))
			fmt.Fprintf(buf, "%s_count{%s} %d\n", name, k, atomic.LoadInt64(&h.count))
		}
	}
}

// RenderText 返回 Prometheus 文本。
func (r *Registry) RenderText() string {
	var sb strings.Builder
	r.Render(&sb)
	return sb.String()
}
