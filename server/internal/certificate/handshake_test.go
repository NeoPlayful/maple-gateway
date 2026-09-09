package certificate

import (
	"crypto/tls"
	"errors"
	"net"
	"testing"
	"time"
)

// startTestTLS 起一个真实 TLS 服务端：GetCertificate 命中 SNI；未命中回退 fallbackCert。
// fallback 传 nil 表示不设 Certificates（miss 将导致握手失败）。
func startTestTLS(t *testing.T, cache *Cache, fallback bool) (addr string, stop func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	cfg.GetCertificate = NewGetter(cache, fallback, nil).GetCertificate
	if fallback {
		loaded := mustLoaded(t, "*.test") // 仅作回退证书，命中不影响
		cfg.Certificates = []tls.Certificate{*loaded.Cert}
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				tc := tls.Server(conn, cfg)
				_ = tc.Handshake()
			}()
		}
	}()
	return ln.Addr().String(), func() { _ = ln.Close(); <-done }
}

// fetchServerName 用 ServerName 连接并返回对方握手证书 CN（失败返回 err）。
func fetchServerName(t *testing.T, addr, sni string) (string, error) {
	t.Helper()
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 2 * time.Second}, "tcp", addr,
		&tls.Config{ServerName: sni, InsecureSkipVerify: true})
	if err != nil {
		return "", err
	}
	defer conn.Close()
	state := conn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return "", errors.New("no peer certificate presented")
	}
	return state.PeerCertificates[0].Subject.CommonName, nil
}

func TestHandshakeSNIRouting(t *testing.T) {
	cache := NewCache()
	cache.Set("shop-a.test", mustLoaded(t, "shop-a.test"))
	cache.Set("shop-b.test", mustLoaded(t, "shop-b.test"))
	addr, stop := startTestTLS(t, cache, true)
	defer stop()

	cnA, err := fetchServerName(t, addr, "shop-a.test")
	if err != nil {
		t.Fatalf("handshake shop-a.test: %v", err)
	}
	if cnA != "shop-a.test" {
		t.Fatalf("expected shop-a.test cert, got CN=%q", cnA)
	}
	cnB, err := fetchServerName(t, addr, "shop-b.test")
	if err != nil {
		t.Fatalf("handshake shop-b.test: %v", err)
	}
	if cnB != "shop-b.test" {
		t.Fatalf("expected shop-b.test cert, got CN=%q", cnB)
	}
}

func TestHandshakeFallbackForUnknownSNI(t *testing.T) {
	cache := NewCache()
	cache.Set("shop-a.test", mustLoaded(t, "shop-a.test"))
	addr, stop := startTestTLS(t, cache, true)
	defer stop()

	// unknown SNI 命中回退证书（*.test）。
	cn, err := fetchServerName(t, addr, "unknown.test")
	if err != nil {
		t.Fatalf("fallback handshake should succeed: %v", err)
	}
	if cn != "*.test" {
		t.Fatalf("expected fallback CN *.test, got %q", cn)
	}
}

func TestHandshakeRejectUnknownSNIWhenNoFallback(t *testing.T) {
	cache := NewCache()
	cache.Set("shop-a.test", mustLoaded(t, "shop-a.test"))
	addr, stop := startTestTLS(t, cache, false) // 无回退证书
	defer stop()

	if _, err := fetchServerName(t, addr, "unknown.test"); err == nil {
		t.Fatal("handshake with unknown SNI and no fallback must fail")
	}
}
