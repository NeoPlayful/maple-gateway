package proxy

import (
	"net/http"
	"testing"
)

func mustTrust(t *testing.T, specs ...string) *ProxyTrust {
	t.Helper()
	tr, err := NewProxyTrust(specs)
	if err != nil {
		t.Fatalf("NewProxyTrust(%v) error: %v", specs, err)
	}
	return tr
}

func TestNewProxyTrust(t *testing.T) {
	t.Run("parses cidr and single ip", func(t *testing.T) {
		tr := mustTrust(t, "10.0.0.0/8", "203.0.113.5", " 192.168.0.0/16 ")
		if !tr.Trusted("10.1.2.3") {
			t.Errorf("10.1.2.3 should be trusted via CIDR")
		}
		if !tr.Trusted("203.0.113.5") {
			t.Errorf("203.0.113.5 should be trusted as exact IP")
		}
		if !tr.Trusted("192.168.99.1") {
			t.Errorf("192.168.99.1 should be trusted via CIDR")
		}
		if tr.Trusted("8.8.8.8") {
			t.Errorf("8.8.8.8 should not be trusted")
		}
	})

	t.Run("rejects malformed spec", func(t *testing.T) {
		if _, err := NewProxyTrust([]string{"not-an-ip"}); err == nil {
			t.Fatal("expected error for malformed spec")
		}
		if _, err := NewProxyTrust([]string{"10.0.0.0/99"}); err == nil {
			t.Fatal("expected error for malformed CIDR")
		}
	})

	t.Run("nil trust trusts nothing", func(t *testing.T) {
		var tr *ProxyTrust
		if tr.Trusted("127.0.0.1") {
			t.Errorf("nil trust must not trust loopback")
		}
		if tr.trustedPeer("127.0.0.1:80") {
			t.Errorf("nil trust must not trust any peer")
		}
	})

	t.Run("private ip not implicitly trusted", func(t *testing.T) {
		tr := mustTrust(t, "10.0.0.0/8")
		if tr.Trusted("192.168.1.5") {
			t.Errorf("192.168.1.5 must not be trusted unless listed")
		}
		if tr.Trusted("127.0.0.1") {
			t.Errorf("loopback must not be trusted unless listed")
		}
	})
}

func TestRealClientIP(t *testing.T) {
	// 组合：单个受信代理 + 一个非受信上游网段。
	tr := mustTrust(t, "10.0.0.0/8")

	cases := []struct {
		name       string
		remoteAddr string
		xff        []string
		xRealIP    string
		want       string
	}{
		{
			name:       "untrusted peer ignores xff entirely",
			remoteAddr: "203.0.113.9:5000",
			xff:        []string{"1.2.3.4"},
			want:       "",
		},
		{
			name:       "trusted peer single hop returns client",
			remoteAddr: "10.0.0.1:5000",
			xff:        []string{"198.51.100.7"},
			want:       "198.51.100.7",
		},
		{
			name:       "trusted peer multi hop skips trusted tail",
			remoteAddr: "10.0.0.1:5000",
			// 最右是本网关自己的受信代理，继续向左找到真实客户端。
			xff:  []string{"198.51.100.7, 10.0.0.5"},
			want: "198.51.100.7",
		},
		{
			name:       "all hops trusted falls back to x-real-ip",
			remoteAddr: "10.0.0.1:5000",
			xff:        []string{"10.0.0.9, 10.0.0.5"},
			xRealIP:    "198.51.100.7",
			want:       "198.51.100.7",
		},
		{
			name:       "no xff falls back to x-real-ip",
			remoteAddr: "10.0.0.1:5000",
			xRealIP:    "198.51.100.7",
			want:       "198.51.100.7",
		},
		{
			name:       "x-real-ip that is a trusted proxy is rejected",
			remoteAddr: "10.0.0.1:5000",
			xRealIP:    "10.0.0.5",
			want:       "",
		},
		{
			name:       "malformed hop aborts derivation",
			remoteAddr: "10.0.0.1:5000",
			xff:        []string{"198.51.100.7, not-an-ip"},
			want:       "",
		},
		{
			name:       "multiple xff header lines merged",
			remoteAddr: "10.0.0.1:5000",
			xff:        []string{"198.51.100.7", "10.0.0.5"},
			want:       "198.51.100.7",
		},
		{
			name:       "spoofed leftmost value behind trusted hop is skipped",
			remoteAddr: "10.0.0.1:5000",
			// 客户端伪造了最左值，但最右受信跳才是准绳——真实客户端取可信跳左侧。
			xff:  []string{"9.9.9.9, 198.51.100.7, 10.0.0.5"},
			want: "198.51.100.7",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := http.NewRequest("GET", "http://example.com/", nil)
			r.RemoteAddr = tc.remoteAddr
			for _, v := range tc.xff {
				r.Header.Add("X-Forwarded-For", v)
			}
			if tc.xRealIP != "" {
				r.Header.Set("X-Real-IP", tc.xRealIP)
			}
			if got := tr.realClientIP(r); got != tc.want {
				t.Fatalf("realClientIP() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestApplyForwardedHeaders(t *testing.T) {
	tr := mustTrust(t, "10.0.0.0/8")

	t.Run("no existing xff, sets peer", func(t *testing.T) {
		h := http.Header{}
		tr.applyForwardedHeaders(h, "192.168.1.10:5000", false)
		if got := h.Get("X-Forwarded-For"); got != "192.168.1.10" {
			t.Fatalf("XFF = %q, want 192.168.1.10", got)
		}
		if got := h.Get("X-Forwarded-Proto"); got != "http" {
			t.Fatalf("Proto = %q, want http", got)
		}
	})

	t.Run("untrusted peer drops spoofed chain", func(t *testing.T) {
		h := http.Header{}
		h.Set("X-Forwarded-For", "1.2.3.4")
		// 对端 203.0.113.9 不在白名单：入站伪造链必须丢弃，仅写对端 IP。
		tr.applyForwardedHeaders(h, "203.0.113.9:5000", false)
		if got := h.Get("X-Forwarded-For"); got != "203.0.113.9" {
			t.Fatalf("XFF = %q, want only peer 203.0.113.9", got)
		}
	})

	t.Run("trusted peer appends to existing chain", func(t *testing.T) {
		h := http.Header{}
		h.Set("X-Forwarded-For", "203.0.113.5")
		tr.applyForwardedHeaders(h, "10.0.0.1:5000", false)
		if got := h.Get("X-Forwarded-For"); got != "203.0.113.5, 10.0.0.1" {
			t.Fatalf("XFF = %q, want chain", got)
		}
	})

	t.Run("tls sets https proto", func(t *testing.T) {
		h := http.Header{}
		tr.applyForwardedHeaders(h, "10.0.0.2:4000", true)
		if got := h.Get("X-Forwarded-Proto"); got != "https" {
			t.Fatalf("Proto = %q, want https", got)
		}
	})

	t.Run("preserves existing proto", func(t *testing.T) {
		h := http.Header{}
		h.Set("X-Forwarded-Proto", "https")
		tr.applyForwardedHeaders(h, "10.0.0.2:4000", false)
		if got := h.Get("X-Forwarded-Proto"); got != "https" {
			t.Fatalf("Proto = %q, want preserved https", got)
		}
	})

	t.Run("nil trust still writes peer but never appends", func(t *testing.T) {
		var nilTrust *ProxyTrust
		h := http.Header{}
		h.Set("X-Forwarded-For", "1.2.3.4")
		nilTrust.applyForwardedHeaders(h, "10.0.0.1:5000", false)
		if got := h.Get("X-Forwarded-For"); got != "10.0.0.1" {
			t.Fatalf("XFF = %q, want only peer 10.0.0.1", got)
		}
	})
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

func TestTruncateField(t *testing.T) {
	short := "hello"
	if got := truncateField(short); got != short {
		t.Errorf("truncateField(%q) = %q, want unchanged", short, got)
	}
	long := make([]byte, maxUAOrHeaderLen+10)
	for i := range long {
		long[i] = 'a'
	}
	got := truncateField(string(long))
	if len(got) != maxUAOrHeaderLen+3 {
		t.Fatalf("len = %d, want %d", len(got), maxUAOrHeaderLen+3)
	}
}
