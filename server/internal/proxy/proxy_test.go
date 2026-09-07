package proxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/internal/router"
)

// 构造一个指向真实 httptest 后端的 resolver。
func resolverFor(t *testing.T, upstream *httptest.Server) router.Resolver {
	t.Helper()
	return router.FromMap(map[string]router.StaticEntry{
		"shop.test": {Scheme: "http", Address: strings.TrimPrefix(upstream.URL, "http://")},
	})
}

func TestProxy_Forward(t *testing.T) {
	// 上游把收到的 XFF 头回显到响应体，便于断言转发头是否补全。
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Upstream", "yes")
		xff := r.Header.Get("X-Forwarded-For")
		io.WriteString(w, "hello upstream|xff="+xff)
	}))
	defer upstream.Close()

	px := New(Config{Resolver: resolverFor(t, upstream)})

	t.Run("route and forward", func(t *testing.T) {
		req := httptest.NewRequest("GET", "http://shop.test/foo?x=1", nil)
		req.Host = "shop.test"
		req.RemoteAddr = "127.0.0.1:55555"
		rec := httptest.NewRecorder()
		px.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("code = %d, want 200", rec.Code)
		}
		if body := rec.Body.String(); body != "hello upstream|xff=127.0.0.1" {
			t.Fatalf("body = %q, want echoed XFF 127.0.0.1", body)
		}
		if rec.Header().Get("X-Upstream") != "yes" {
			t.Fatalf("upstream header not forwarded")
		}
	})

	t.Run("unknown host returns 404", func(t *testing.T) {
		req := httptest.NewRequest("GET", "http://unknown.test/", nil)
		rec := httptest.NewRecorder()
		px.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("code = %d, want 404", rec.Code)
		}
	})

	t.Run("request body is forwarded", func(t *testing.T) {
		up2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			io.WriteString(w, string(body))
		}))
		defer up2.Close()
		px2 := New(Config{Resolver: resolverFor(t, up2)})

		req := httptest.NewRequest("POST", "http://shop.test/", strings.NewReader("payload-data"))
		req.Host = "shop.test"
		rec := httptest.NewRecorder()
		px2.ServeHTTP(rec, req)
		if rec.Body.String() != "payload-data" {
			t.Fatalf("body forwarded = %q", rec.Body.String())
		}
	})
}

func TestProxy_UpstreamDown(t *testing.T) {
	// 后端直接关闭（空监听），转发应返回 502 而非崩溃。
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	addr := strings.TrimPrefix(dead.URL, "http://")
	dead.Close()

	px := New(Config{Resolver: router.FromMap(map[string]router.StaticEntry{
		"shop.test": {Scheme: "http", Address: addr},
	})})

	req := httptest.NewRequest("GET", "http://shop.test/", nil)
	req.Host = "shop.test"
	rec := httptest.NewRecorder()
	px.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("code = %d, want 502", rec.Code)
	}
}

func TestProxy_ContextCancel(t *testing.T) {
	// 客户端取消：上游已建立连接处理时取消，ctx 应向传播，上游观察 Done。
	started := make(chan struct{})
	cancelled := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started) // 上游开始处理
		<-r.Context().Done()
		close(cancelled)
	}))
	defer upstream.Close()

	px := New(Config{Resolver: resolverFor(t, upstream)})

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest("GET", "http://shop.test/slow", nil).WithContext(ctx)
	req.Host = "shop.test"
	req.RemoteAddr = "127.0.0.1:55556"

	done := make(chan struct{})
	go func() {
		px.ServeHTTP(httptest.NewRecorder(), req)
		close(done)
	}()

	select {
	case <-started:
		// 上游已开始处理，此时取消请求。
		cancel()
	case <-time.After(2 * time.Second):
		t.Fatal("upstream did not start within 2s")
	}

	select {
	case <-cancelled:
		// ctx 取消已传播到上游
	case <-time.After(2 * time.Second):
		t.Fatal("upstream ctx not cancelled after client cancel")
	}
}
