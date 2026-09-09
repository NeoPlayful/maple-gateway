package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/internal/logs"
)

// echoRIDUpstream 把收到的 X-Request-Id 回显在响应体，便于断言透传。
func echoRIDUpstream() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "rid="+r.Header.Get(RequestIDHeader))
	}))
}

func TestProxy_RequestID_Generated(t *testing.T) {
	upstream := echoRIDUpstream()
	defer upstream.Close()

	px := New(Config{Resolver: resolverFor(t, upstream)})
	req := httptest.NewRequest("GET", "http://shop.test/a", nil)
	req.Host = "shop.test"
	rec := httptest.NewRecorder()
	px.ServeHTTP(rec, req)

	// 网关生成请求 ID：响应头可见、上游也收到同一 ID。
	rid := rec.Header().Get(RequestIDHeader)
	if len(rid) != 32 {
		t.Fatalf("generated rid len = %d, want 32", len(rid))
	}
	if body := rec.Body.String(); body != "rid="+rid {
		t.Fatalf("upstream rid = %q, want %q", body, "rid="+rid)
	}
}

func TestProxy_RequestID_Passthrough(t *testing.T) {
	upstream := echoRIDUpstream()
	defer upstream.Close()

	px := New(Config{Resolver: resolverFor(t, upstream)})
	req := httptest.NewRequest("GET", "http://shop.test/a", nil)
	req.Host = "shop.test"
	req.Header.Set(RequestIDHeader, "client-supplied-id-123")
	rec := httptest.NewRecorder()
	px.ServeHTTP(rec, req)

	// 客户端已带 → 透传，不重新生成。
	if got := rec.Header().Get(RequestIDHeader); got != "client-supplied-id-123" {
		t.Fatalf("response rid = %q, want passthrough", got)
	}
	if body := rec.Body.String(); body != "rid=client-supplied-id-123" {
		t.Fatalf("upstream rid = %q, want passthrough", body)
	}
}

func TestProxy_RequestID_InAccessLog(t *testing.T) {
	upstream := echoRIDUpstream()
	defer upstream.Close()

	access := logs.NewAccessLog(10)
	px := New(Config{Resolver: resolverFor(t, upstream), AccessLog: access})
	req := httptest.NewRequest("GET", "http://shop.test/b", nil)
	req.Host = "shop.test"
	rec := httptest.NewRecorder()
	px.ServeHTTP(rec, req)

	rid := rec.Header().Get(RequestIDHeader)
	entries := access.Query("", 0, rid, time.Time{}, time.Time{}, 10, 0)
	if len(entries) != 1 {
		t.Fatalf("access log by request_id got %d entries, want 1", len(entries))
	}
	if entries[0].RequestID != rid {
		t.Fatalf("access entry request_id = %q, want %q", entries[0].RequestID, rid)
	}
}
