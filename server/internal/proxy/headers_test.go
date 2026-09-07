package proxy

import (
	"net/http"
	"testing"
)

func TestApplyForwardedHeaders(t *testing.T) {
	t.Run("no existing xff, sets peer", func(t *testing.T) {
		h := http.Header{}
		applyForwardedHeaders(h, "192.168.1.10:5000", false)
		if got := h.Get("X-Forwarded-For"); got != "192.168.1.10" {
			t.Fatalf("XFF = %q, want 192.168.1.10", got)
		}
		if got := h.Get("X-Forwarded-Proto"); got != "http" {
			t.Fatalf("Proto = %q, want http", got)
		}
	})

	t.Run("appends to existing chain", func(t *testing.T) {
		h := http.Header{}
		h.Set("X-Forwarded-For", "203.0.113.5")
		applyForwardedHeaders(h, "192.168.1.10:5000", false)
		if got := h.Get("X-Forwarded-For"); got != "203.0.113.5, 192.168.1.10" {
			t.Fatalf("XFF = %q, want chain", got)
		}
	})

	t.Run("tls sets https proto", func(t *testing.T) {
		h := http.Header{}
		applyForwardedHeaders(h, "10.0.0.2:4000", true)
		if got := h.Get("X-Forwarded-Proto"); got != "https" {
			t.Fatalf("Proto = %q, want https", got)
		}
	})

	t.Run("preserves existing proto", func(t *testing.T) {
		h := http.Header{}
		h.Set("X-Forwarded-Proto", "https")
		applyForwardedHeaders(h, "10.0.0.2:4000", false)
		if got := h.Get("X-Forwarded-Proto"); got != "https" {
			t.Fatalf("Proto = %q, want preserved https", got)
		}
	})
}

func TestTrustedProxyAddr(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1:80":     true,
		"[::1]:80":         true,
		"192.168.1.5:9999": true,
		"10.0.0.3:80":      true,
		"8.8.8.8:53":       false,
		"1.2.3.4:80":       false,
	}
	for addr, want := range cases {
		if got := trustedProxyAddr(addr); got != want {
			t.Errorf("trustedProxyAddr(%q) = %v, want %v", addr, got, want)
		}
	}
}

func TestClientIP(t *testing.T) {
	cases := map[string]string{
		"127.0.0.1:80":     "127.0.0.1",
		"[::1]:443":        "::1",
		"192.168.0.1:8080": "192.168.0.1",
	}
	for in, want := range cases {
		if got := clientIP(in); got != want {
			t.Errorf("clientIP(%q) = %q, want %q", in, got, want)
		}
	}
}
