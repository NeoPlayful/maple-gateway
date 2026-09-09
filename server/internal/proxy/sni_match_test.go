package proxy

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/NeoPlayful/maple-gateway/server/internal/router"
)

func TestProxy_SNIHostMismatch(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	// 启用 SNI/Host 校验。
	px := New(Config{
		Resolver: router.FromMap(map[string]router.StaticEntry{
			"shop-a.test": {Scheme: "http", Address: strings.TrimPrefix(upstream.URL, "http://")},
		}),
		EnforceSNIHostMatch: true,
	})

	t.Run("sni equals host forwards normally", func(t *testing.T) {
		req := httptest.NewRequest("GET", "http://shop-a.test/", nil)
		req.Host = "shop-a.test"
		// 模拟 TLS：SNI 与 Host 一致。
		req.TLS = &tls.ConnectionState{ServerName: "shop-a.test"}
		rec := httptest.NewRecorder()
		px.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("sni==host code = %d, want 200", rec.Code)
		}
	})

	t.Run("sni differs from host returns 421", func(t *testing.T) {
		req := httptest.NewRequest("GET", "http://shop-a.test/", nil)
		req.Host = "shop-a.test"
		req.TLS = &tls.ConnectionState{ServerName: "shop-b.test"} // 握手用 shop-b 证书
		rec := httptest.NewRecorder()
		px.ServeHTTP(rec, req)
		if rec.Code != http.StatusMisdirectedRequest {
			t.Fatalf("sni!=host code = %d, want 421", rec.Code)
		}
	})
}

func TestProxy_SNIHostMismatchDisabled(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	// 关闭校验：SNI 与 Host 不一致仍按 Host 转发（Cloudflare/global 语义）。
	px := New(Config{
		Resolver: router.FromMap(map[string]router.StaticEntry{
			"shop-a.test": {Scheme: "http", Address: strings.TrimPrefix(upstream.URL, "http://")},
		}),
		EnforceSNIHostMatch: false,
	})
	req := httptest.NewRequest("GET", "http://shop-a.test/", nil)
	req.Host = "shop-a.test"
	req.TLS = &tls.ConnectionState{ServerName: "shop-b.test"}
	rec := httptest.NewRecorder()
	px.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("enforce disabled code = %d, want 200", rec.Code)
	}
}
