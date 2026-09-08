package dashboard

import (
	"testing"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/internal/metrics"
)

func TestTraffic_Aggregation(t *testing.T) {
	reg := metrics.NewRegistry()
	ts := metrics.NewTimeSeries(reg, metrics.SeriesConfig{Window: 15 * time.Second, Buckets: 10})
	base := time.Now()

	// 窗口1：3 个 200 + 1 个 500 + 1 个限流命中。
	reg.Inc("maple_requests_total", map[string]string{"host": "a.com", "status": "200"})
	reg.Inc("maple_requests_total", map[string]string{"host": "a.com", "status": "200"})
	reg.Inc("maple_requests_total", map[string]string{"host": "a.com", "status": "200"})
	reg.Inc("maple_requests_total", map[string]string{"host": "a.com", "status": "500"})
	reg.Inc("maple_rate_limit_hits_total", map[string]string{"scope": "service"})
	ts.SampleAt(base.Add(15 * time.Second))

	// 窗口2：2 个 200 + 1 个 rejected。
	reg.Inc("maple_requests_total", map[string]string{"host": "a.com", "status": "200"})
	reg.Inc("maple_requests_total", map[string]string{"host": "a.com", "status": "200"})
	reg.Inc("maple_requests_total", map[string]string{"host": "a.com", "status": "rejected"})
	ts.SampleAt(base.Add(30 * time.Second))

	got := Traffic(ts, 10)
	if len(got) != 2 {
		t.Fatalf("want 2 points, got %d: %+v", len(got), got)
	}
	// 最新窗口（index0）requests=3, errors=1(rejected)。
	if got[0].Requests != 3 || got[0].Errors != 1 {
		t.Fatalf("latest window want req=3 err=1, got %+v", got[0])
	}
	if got[0].RateLimitHits != 0 {
		t.Fatalf("latest window should have 0 ratelimit hits, got %+v", got[0])
	}
	// 前窗口 requests=4, errors=1(500), rate limit=1。
	if got[1].Requests != 4 || got[1].Errors != 1 || got[1].RateLimitHits != 1 {
		t.Fatalf("prev window want req=4 err=1 rl=1, got %+v", got[1])
	}
}

func TestTraffic_NilSeries(t *testing.T) {
	if got := Traffic(nil, 10); len(got) != 0 {
		t.Fatalf("nil series should return empty, got %v", got)
	}
}

func TestLatency_Aggregation(t *testing.T) {
	reg := metrics.NewRegistry()
	ts := metrics.NewTimeSeries(reg, metrics.SeriesConfig{Window: 15 * time.Second, Buckets: 10})
	base := time.Now()

	reg.ObserveDuration("maple_request_duration_seconds", 100*time.Millisecond,
		map[string]string{"host": "a.com"})
	ts.SampleAt(base.Add(15 * time.Second))

	got := Latency(ts, 10)
	if len(got) != 1 {
		t.Fatalf("want 1 point, got %d", len(got))
	}
	if got[0].Count != 1 {
		t.Fatalf("want count 1, got %+v", got[0])
	}
	if got[0].AvgMS < 90 || got[0].AvgMS > 110 {
		t.Fatalf("avg should be ~100ms, got %+v", got[0])
	}
	if got[0].P95MS < 90 || got[0].P95MS > 110 {
		t.Fatalf("p95 should be ~100ms, got %+v", got[0])
	}
}

func TestIsErrLabel(t *testing.T) {
	cases := map[string]bool{
		"host=a.com,status=500":   true,
		"host=a.com,status=502":   true,
		"host=a.com,status=rejected": true,
		"host=a.com,status=200":   false,
		"host=a.com,status=404":   false,
		"host=a.com,status=503":   true,
	}
	for labels, want := range cases {
		if got := isErrLabel(labels); got != want {
			t.Errorf("isErrLabel(%q)=%v want %v", labels, got, want)
		}
	}
}
