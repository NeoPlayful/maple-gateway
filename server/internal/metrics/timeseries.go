// 进程内环形时间桶：对 Registry 做周期快照差分，保留最近若干窗口的增量数据。
//
// 作为 Dashboard 趋势图（traffic / errors / latency）的数据源。counter / histogram
// 存相邻快照的差分（窗口内新增量），gauge 存瞬时采样值。重启清零，无持久化。
package metrics

import (
	"context"
	"sync"
	"time"
)

// SeriesConfig 时间桶配置。
type SeriesConfig struct {
	// Window 单个窗口时长。默认 15s。
	Window time.Duration
	// Buckets 保留窗口数（总跨度 = Window*Buckets）。默认 120（30 分钟）。
	Buckets int
}

func (c *SeriesConfig) window() time.Duration {
	if c.Window > 0 {
		return c.Window
	}
	return 15 * time.Second
}

func (c *SeriesConfig) buckets() int {
	if c.Buckets > 0 {
		return c.Buckets
	}
	return 120
}

// WindowPoint 一个窗口的数据。
type WindowPoint struct {
	// Time 窗口推进时刻（Unix 秒）。
	Time int64
	// Counters[metric][labels] 窗口内新增计数（差分）。labels 为 "k=v,k=v" 编码。
	Counters map[string]map[string]int64
	// Histograms[metric][labels] 窗口内延迟累计（sum/count 差分）。
	Histograms map[string]map[string]HistoSnap
	// Gauges[metric][labels] 窗口采样时刻的瞬时值。
	Gauges map[string]map[string]int64
}

// TimeSeries 环形时间桶。
type TimeSeries struct {
	mu      sync.RWMutex
	reg     *Registry
	window  time.Duration
	buckets int

	points []*WindowPoint // 最新在前
	prev   Snapshot       // 上一次快照（差分基准）
	prevAt time.Time
}

// NewTimeSeries 构造，并做一次基线快照。
func NewTimeSeries(reg *Registry, cfg SeriesConfig) *TimeSeries {
	if reg == nil {
		reg = NewRegistry()
	}
	return &TimeSeries{
		reg:     reg,
		window:  cfg.window(),
		buckets: cfg.buckets(),
		prev:    reg.Snapshot(),
		prevAt:  time.Now(),
	}
}

// Sample 采样一次并推进窗口。
//
// 距上次采样不足一个窗口时，把差分并入最新窗口（避免外部以过密节奏调用时
// 产生大量半空窗口）；否则新建窗口并裁剪超出保留数的旧窗口。
func (ts *TimeSeries) Sample() {
	ts.SampleAt(time.Now())
}

// SampleAt 以给定时刻采样。测试用注入时间以模拟跨窗口推进。
func (ts *TimeSeries) SampleAt(now time.Time) {
	cur := ts.reg.Snapshot()

	ts.mu.Lock()
	defer ts.mu.Unlock()

	if len(ts.points) > 0 && now.Sub(ts.prevAt) < ts.window {
		mergeInto(ts.points[0], ts.diff(cur), now)
	} else {
		p := ts.diff(cur)
		p.Time = now.Unix()
		ts.points = append([]*WindowPoint{p}, ts.points...)
		if len(ts.points) > ts.buckets {
			ts.points = ts.points[:ts.buckets]
		}
	}
	ts.prev = cur
	ts.prevAt = now
}

// diff 基于当前 prev 计算 cur 的增量，返回新窗口数据。
func (ts *TimeSeries) diff(cur Snapshot) *WindowPoint {
	p := &WindowPoint{
		Counters:   map[string]map[string]int64{},
		Histograms: map[string]map[string]HistoSnap{},
		Gauges:     map[string]map[string]int64{},
	}
	for name, curLabels := range cur.Counters {
		prevLabels := ts.prev.Counters[name]
		out := map[string]int64{}
		for k, cv := range curLabels {
			delta := cv
			if pv, ok := prevLabels[k]; ok {
				delta = cv - pv
			}
			if delta > 0 {
				out[k] = delta
			}
		}
		if len(out) > 0 {
			p.Counters[name] = out
		}
	}
	for name, curLabels := range cur.Histograms {
		prevLabels := ts.prev.Histograms[name]
		out := map[string]HistoSnap{}
		for k, ch := range curLabels {
			ph, ok := prevLabels[k]
			if !ok {
				ph = HistoSnap{}
			}
			dc := ch.Count - ph.Count
			if dc < 0 {
				dc = 0
			}
			ds := ch.Sum - ph.Sum
			if ds < 0 {
				ds = 0
			}
			if dc > 0 {
				out[k] = HistoSnap{Sum: ds, Count: dc}
			}
		}
		if len(out) > 0 {
			p.Histograms[name] = out
		}
	}
	return p
}

// mergeInto 把新差分并入既有点（同一窗口内多次采样），并刷新其时间。
func mergeInto(dst *WindowPoint, diff *WindowPoint, now time.Time) {
	for name, labels := range diff.Counters {
		d := dst.Counters[name]
		if d == nil {
			d = map[string]int64{}
			dst.Counters[name] = d
		}
		for k, v := range labels {
			d[k] += v
		}
	}
	for name, labels := range diff.Histograms {
		d := dst.Histograms[name]
		if d == nil {
			d = map[string]HistoSnap{}
			dst.Histograms[name] = d
		}
		for k, v := range labels {
			prev := d[k]
			prev.Count += v.Count
			prev.Sum += v.Sum
			d[k] = prev
		}
	}
	dst.Time = now.Unix()
}

// Range 返回最近 n 个窗口的深拷贝（最新在前）。n<=0 返回全部。
func (ts *TimeSeries) Range(n int) []*WindowPoint {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	if n <= 0 || n > len(ts.points) {
		n = len(ts.points)
	}
	out := make([]*WindowPoint, 0, n)
	for i := 0; i < n; i++ {
		src := ts.points[i]
		cp := &WindowPoint{
			Time:       src.Time,
			Counters:   map[string]map[string]int64{},
			Histograms: map[string]map[string]HistoSnap{},
			Gauges:     map[string]map[string]int64{},
		}
		for name, labels := range src.Counters {
			m := make(map[string]int64, len(labels))
			for k, v := range labels {
				m[k] = v
			}
			cp.Counters[name] = m
		}
		for name, labels := range src.Histograms {
			m := make(map[string]HistoSnap, len(labels))
			for k, v := range labels {
				m[k] = v
			}
			cp.Histograms[name] = m
		}
		for name, labels := range src.Gauges {
			m := make(map[string]int64, len(labels))
			for k, v := range labels {
				m[k] = v
			}
			cp.Gauges[name] = m
		}
		out = append(out, cp)
	}
	return out
}

// Run 启动后台采样：每 window 采样一次，直到 ctx 取消。供 main 驱动时间桶。
func (ts *TimeSeries) Run(ctx context.Context) {
	t := time.NewTicker(ts.window)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			ts.Sample()
		}
	}
}
