package gateway

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
)

// TestInFlightLimiter_RejectsOverLimit 验证：占满上限后，再来一个请求即被拒为 503。
func TestInFlightLimiter_RejectsOverLimit(t *testing.T) {
	const limit = 3
	release := make(chan struct{})
	var concurrent int64
	entered := make(chan struct{})

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt64(&concurrent, 1) <= limit {
			entered <- struct{}{} // 只在「占位」的前 limit 个上报
		}
		<-release
		atomic.AddInt64(&concurrent, -1)
		w.WriteHeader(200)
	})
	h := NewInFlightLimiter(limit, next)

	var wg sync.WaitGroup
	for i := 0; i < limit; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
		}()
	}
	// 等前 limit 个请求真正进入 handler（占满所有槽位）。
	for i := 0; i < limit; i++ {
		<-entered
	}

	// 此时槽位已满：新请求应被立即拒绝，不进入 handler。
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("槽位占满后应返回 503，实际 %d", rec.Code)
	}
	if got := h.InFlight(); got != limit {
		t.Fatalf("在途数应为 %d，实际 %d", limit, got)
	}

	close(release)
	wg.Wait()
	if got := h.InFlight(); got != 0 {
		t.Fatalf("全部结束后在途数应归零，实际 %d", got)
	}
}

// TestInFlightLimiter_ZeroMeansUnlimited 验证 limit<=0 时全部放行。
func TestInFlightLimiter_ZeroMeansUnlimited(t *testing.T) {
	h := NewInFlightLimiter(0, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	for i := 0; i < 5; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
		if rec.Code != 200 {
			t.Fatalf("limit=0 应全放行，得到 %d", rec.Code)
		}
	}
}
