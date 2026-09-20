package gateway

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestBodyLimiter_RejectsOverLimit 验证超限请求体在读取时报 MaxBytesError。
func TestBodyLimiter_RejectsOverLimit(t *testing.T) {
	var readErr error
	h := NewBodyLimiter(4, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, readErr = io.ReadAll(r.Body)
		w.WriteHeader(200)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/", strings.NewReader("1234567890")))
	var mbErr *http.MaxBytesError
	if !errors.As(readErr, &mbErr) {
		t.Fatalf("超限应返回 MaxBytesError，实际 %v", readErr)
	}
}

// TestBodyLimiter_UnderLimit 验证未超限时正常读取。
func TestBodyLimiter_UnderLimit(t *testing.T) {
	var got string
	h := NewBodyLimiter(100, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = string(b)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/", strings.NewReader("hello")))
	if got != "hello" {
		t.Fatalf("body = %q, want hello", got)
	}
}

// TestBodyLimiter_ZeroMeansUnlimited 验证 limit<=0 时不限制。
func TestBodyLimiter_ZeroMeansUnlimited(t *testing.T) {
	var got int
	h := NewBodyLimiter(0, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = len(b)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/", strings.NewReader(strings.Repeat("x", 1000))))
	if got != 1000 {
		t.Fatalf("body len = %d, want 1000", got)
	}
}

// TestBodyLimiter_SetLimitHot 验证 SetLimit 热更新即时生效。
func TestBodyLimiter_SetLimitHot(t *testing.T) {
	var readErr error
	h := NewBodyLimiter(0, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, readErr = io.ReadAll(r.Body)
	}))
	// 初始不限：大 body 应通过。
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/", strings.NewReader(strings.Repeat("x", 100))))
	if readErr != nil {
		t.Fatalf("未设限时不应报错，实际 %v", readErr)
	}
	// 收紧到 4：应立即生效。
	h.SetLimit(4)
	readErr = nil
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/", strings.NewReader("1234567890")))
	var mbErr *http.MaxBytesError
	if !errors.As(readErr, &mbErr) {
		t.Fatalf("收紧后应返回 MaxBytesError，实际 %v", readErr)
	}
}
