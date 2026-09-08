package metrics

import (
	"testing"
	"time"
)

// 直接驱动 SampleAt（不依赖真实 tick），推进时间模拟跨窗口。
func TestTimeSeries_CounterDiff(t *testing.T) {
	reg := NewRegistry()
	ts := NewTimeSeries(reg, SeriesConfig{Window: 10 * time.Second, Buckets: 4})
	base := time.Now()

	// 基线（构造时已采样一次空快照）。
	reg.Inc("maple_requests_total", map[string]string{"host": "a.com", "status": "200"})
	reg.Inc("maple_requests_total", map[string]string{"host": "a.com", "status": "200"})
	ts.SampleAt(base.Add(10 * time.Second))

	reg.Inc("maple_requests_total", map[string]string{"host": "a.com", "status": "500"})
	ts.SampleAt(base.Add(20 * time.Second))

	pts := ts.Range(0)
	if len(pts) != 2 {
		t.Fatalf("want 2 windows, got %d", len(pts))
	}
	// 最新窗口（index 0）只含新增的 500。
	latest := pts[0].Counters["maple_requests_total"]
	if latest["host=a.com,status=500"] != 1 {
		t.Fatalf("latest window should contain 1x500, got %+v", latest)
	}
	if v := latest["host=a.com,status=200"]; v != 0 {
		t.Fatalf("latest window should not re-count 200, got %d", v)
	}
	// 上一个窗口含 2x200。
	prev := pts[1].Counters["maple_requests_total"]
	if prev["host=a.com,status=200"] != 2 {
		t.Fatalf("prev window should contain 2x200, got %+v", prev)
	}
}

func TestTimeSeries_HistogramDiff(t *testing.T) {
	reg := NewRegistry()
	ts := NewTimeSeries(reg, SeriesConfig{Window: 10 * time.Second, Buckets: 4})

	base := time.Now()
	reg.ObserveDuration("maple_request_duration_seconds", 100*time.Millisecond,
		map[string]string{"host": "a.com"})
	ts.SampleAt(base.Add(10 * time.Second))

	reg.ObserveDuration("maple_request_duration_seconds", 200*time.Millisecond,
		map[string]string{"host": "a.com"})
	ts.SampleAt(base.Add(20 * time.Second))

	pts := ts.Range(0)
	if len(pts) != 2 {
		t.Fatalf("want 2 windows, got %d", len(pts))
	}
	latest := pts[0].Histograms["maple_request_duration_seconds"]["host=a.com"]
	if latest.Count != 1 {
		t.Fatalf("latest window count should be 1, got %d", latest.Count)
	}
	if latest.Sum < 0.15 || latest.Sum > 0.25 {
		t.Fatalf("latest window sum ~0.2, got %v", latest.Sum)
	}
	prev := pts[1].Histograms["maple_request_duration_seconds"]["host=a.com"]
	if prev.Count != 1 || prev.Sum < 0.05 || prev.Sum > 0.15 {
		t.Fatalf("prev window should hold 100ms sample, got %+v", prev)
	}
}

func TestTimeSeries_ShortIntervalMerge(t *testing.T) {
	reg := NewRegistry()
	// window 很大，两次紧邻 Sample 应并入同一窗口。
	ts := NewTimeSeries(reg, SeriesConfig{Window: time.Hour, Buckets: 8})

	reg.Inc("c", map[string]string{"a": "1"})
	ts.Sample()
	reg.Inc("c", map[string]string{"a": "1"})
	ts.Sample()

	pts := ts.Range(0)
	if len(pts) != 1 {
		t.Fatalf("short interval should merge into one window, got %d", len(pts))
	}
	if pts[0].Counters["c"]["a=1"] != 2 {
		t.Fatalf("merged window should total 2, got %+v", pts[0].Counters)
	}
}

func TestTimeSeries_CapBuckets(t *testing.T) {
	reg := NewRegistry()
	ts := NewTimeSeries(reg, SeriesConfig{Window: 10 * time.Second, Buckets: 3})
	base := time.Now()
	for i := 0; i < 10; i++ {
		reg.Inc("c", nil)
		ts.SampleAt(base.Add(time.Duration(i+1) * 10 * time.Second))
	}
	if got := len(ts.Range(0)); got != 3 {
		t.Fatalf("want capped at 3 buckets, got %d", got)
	}
}
