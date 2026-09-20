package metrics

import (
	"strings"
	"testing"
	"time"
)

func TestRegistry_CounterAndGauge(t *testing.T) {
	r := NewRegistry()
	r.Inc("maple_requests_total", map[string]string{"host": "a.com", "status": "200"})
	r.Inc("maple_requests_total", map[string]string{"host": "a.com", "status": "200"})
	r.Inc("maple_requests_total", map[string]string{"host": "b.com", "status": "404"})
	r.SetGauge("maple_active_connections", 3, nil)

	out := r.RenderText()
	if !strings.Contains(out, `maple_requests_total{host=a.com,status=200} 2`) {
		t.Fatalf("counter not rendered: %s", out)
	}
	if !strings.Contains(out, `maple_active_connections{} 3`) {
		t.Fatalf("gauge not rendered: %s", out)
	}
}

// TestRegistry_RenderReleasesReadLock 回归：Render 必须释放读锁。
// 修复前 Render 泄漏 RLock，导致其后任何指标写入永久阻塞——线上表现为一次
// /metrics 抓取后，数据面每个请求在写指标处卡死、在途计数涨满触发全量 503。
// 写入放 goroutine 里，回归时只会超时失败而非整个测试挂死。
func TestRegistry_RenderReleasesReadLock(t *testing.T) {
	r := NewRegistry()
	r.Inc("maple_requests_total", map[string]string{"host": "a.com"})
	_ = r.RenderText() // 一次抓取

	done := make(chan struct{})
	go func() {
		r.Inc("maple_requests_total", map[string]string{"host": "b.com"})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Render 后写入指标被永久阻塞：Render 泄漏了读锁")
	}
}

func TestRegistry_Histogram(t *testing.T) {
	r := NewRegistry()
	start := time.Now()
	r.ObserveDuration("maple_request_duration_seconds", 1*time.Millisecond,
		map[string]string{"host": "a.com"})
	r.ObserveDuration("maple_request_duration_seconds", 3*time.Second,
		map[string]string{"host": "a.com"})
	_ = start

	out := r.RenderText()
	if !strings.Contains(out, "maple_request_duration_seconds_count{host=a.com} 2") {
		t.Fatalf("histogram count wrong:\n%s", out)
	}
	if !strings.Contains(out, `le="+Inf"} 2`) {
		t.Fatalf("+Inf bucket wrong:\n%s", out)
	}
	// 3s 落在 5 桶后，+Inf 前应已有累计 2。
	if !strings.Contains(out, "maple_request_duration_seconds_sum{host=a.com} 3.001") {
		t.Fatalf("sum wrong:\n%s", out)
	}
}
