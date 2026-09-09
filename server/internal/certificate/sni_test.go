package certificate

import (
	"crypto/tls"
	"errors"
	"strings"
	"testing"
	"time"
)

// mustLoaded 生成覆盖 host 的自签证书并组装 Loaded 缓存项。
func mustLoaded(t *testing.T, host string) *Loaded {
	t.Helper()
	now := time.Now()
	certPEM, keyPEM, _ := genCert(t, []string{host}, now.Add(-time.Hour), now.Add(24*time.Hour))
	pair, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		t.Fatal(err)
	}
	leaf, _ := parseLeaf(certPEM)
	return &Loaded{Hostname: host, Cert: &pair, Leaf: leaf, Expiry: leaf.NotAfter}
}

func TestGetterHitBySNI(t *testing.T) {
	c := NewCache()
	c.Set("shop-a.test", mustLoaded(t, "shop-a.test"))
	c.Set("shop-b.test", mustLoaded(t, "shop-b.test"))

	g := NewGetter(c, false, nil)
	cert, err := g.GetCertificate(&tls.ClientHelloInfo{ServerName: "shop-a.test"})
	if err != nil {
		t.Fatalf("GetCertificate: %v", err)
	}
	if cert == nil || len(cert.Certificate) == 0 {
		t.Fatal("expected a certificate for shop-a.test")
	}
	leaf, err := parseLeaf(pemEncodeCertDER(cert.Certificate[0]))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(leaf.Subject.CommonName, "shop-a.test") {
		t.Fatalf("expected shop-a.test cert, got %q", leaf.Subject.CommonName)
	}
}

func TestGetterMissNoFallback(t *testing.T) {
	c := NewCache()
	c.Set("shop-a.test", mustLoaded(t, "shop-a.test"))
	g := NewGetter(c, false, nil)

	if _, err := g.GetCertificate(&tls.ClientHelloInfo{ServerName: "unknown.test"}); !errors.Is(err, ErrUnknownSNI) {
		t.Fatalf("expected ErrUnknownSNI, got %v", err)
	}
	if _, err := g.GetCertificate(&tls.ClientHelloInfo{ServerName: ""}); !errors.Is(err, ErrNoSNI) {
		t.Fatalf("expected ErrNoSNI, got %v", err)
	}
}

func TestGetterMissWithFallbackReturnsNil(t *testing.T) {
	c := NewCache()
	c.Set("shop-a.test", mustLoaded(t, "shop-a.test"))
	g := NewGetter(c, true, nil)

	// fallback=true：miss 返回 (nil,nil)，由 tls.Config.Certificates[0] 兜底。
	if cert, err := g.GetCertificate(&tls.ClientHelloInfo{ServerName: "unknown.test"}); err != nil || cert != nil {
		t.Fatalf("fallback miss should return (nil,nil), got cert!=nil=%v err=%v", cert != nil, err)
	}
	if cert, err := g.GetCertificate(&tls.ClientHelloInfo{ServerName: ""}); err != nil || cert != nil {
		t.Fatalf("empty SNI fallback should return (nil,nil), got cert!=nil=%v err=%v", cert != nil, err)
	}
}

func TestGetterNormalizesSNI(t *testing.T) {
	c := NewCache()
	c.Set("shop-a.test", mustLoaded(t, "shop-a.test"))
	g := NewGetter(c, false, nil)

	if cert, err := g.GetCertificate(&tls.ClientHelloInfo{ServerName: "SHOP-A.TEST:443"}); err != nil || cert == nil {
		t.Fatalf("uppercase+port SNI should hit after normalize, cert!=nil=%v err=%v", cert != nil, err)
	}
}

func TestGetterOnMissHook(t *testing.T) {
	c := NewCache()
	c.Set("shop-a.test", mustLoaded(t, "shop-a.test"))
	called := false
	g := NewGetter(c, false, func(sn string, usedFallback bool) {
		called = true
		if sn != "nope.test" {
			t.Fatalf("expected onMiss sn=nope.test, got %q", sn)
		}
		if usedFallback {
			t.Fatal("expected usedFallback=false")
		}
	})
	_, _ = g.GetCertificate(&tls.ClientHelloInfo{ServerName: "nope.test"})
	if !called {
		t.Fatal("onMiss hook not called on cache miss")
	}
}
