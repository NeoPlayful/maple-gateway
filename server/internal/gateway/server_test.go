package gateway

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/zap"
)

// writeSelfSigned 生成覆盖 host 的自签证书，写入临时文件，返回 (certFile, keyFile)。
func writeSelfSigned(t *testing.T, host string) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: host},
		DNSNames:     []string{host},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	certFile := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	return certFile, keyFile
}

// tlsGetterFromHTTPSListener 取出 HTTPS 监听器的 tls.Config（当前只建一个 HTTPS 监听）。
func tlsGetterFromHTTPSListener(dp *DataPlane) *tls.Config {
	if len(dp.listeners) != 1 || !dp.listeners[0].tls {
		return nil
	}
	return dp.listeners[0].server.TLSConfig
}

// minDirectConfig 构造一个 minimal direct 数据平面配置（仅 HTTPS，无 HTTP）。
func minDirectConfig() DataPlaneConfig {
	return DataPlaneConfig{
		HTTPSAddress: ":10443",
		TLSMode:      TLSModeDirect,
		Logger:       zap.NewNop(),
	}
}

// TestDirectWithGetterNeverInstallsStaticFallback: direct + Getter 时，config 静态证书
// （Certificates）不得装入——数据面只认 DB 证书（GetCertificate），不出现共用一张证书。
func TestDirectWithGetterNeverInstallsStaticFallback(t *testing.T) {
	dummy := func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
		return nil, errors.New("unused")
	}
	cfg := minDirectConfig()
	cfg.GetCertificate = dummy
	// 即便 config 配了静态证书，direct 也不应装载它。
	cfg.CertFile = "unused-cert.pem"
	cfg.KeyFile = "unused-key.pem"

	dp := NewDataPlane(cfg)
	tc := tlsGetterFromHTTPSListener(dp)
	if tc == nil {
		t.Fatal("expected an HTTPS tls.Config")
	}
	if tc.GetCertificate == nil {
		t.Fatal("direct mode should set GetCertificate when provided")
	}
	if len(tc.Certificates) != 0 {
		t.Fatalf("direct mode must not load config static certificates, got %d", len(tc.Certificates))
	}
}

// TestDirectWithoutSourceRejectsAllHandshakes: 证书源缺失（无 DB/key → certSvc nil →
// 无 Getter）时，进程继续存活，但任何握手都被恒拒（不向客户端发证书、不共用 config 证书）。
func TestDirectWithoutSourceRejectsAllHandshakes(t *testing.T) {
	cfg := minDirectConfig() // no GetCertificate: simulates absent certSvc
	cfg.CertFile = "unused-cert.pem"
	cfg.KeyFile = "unused-key.pem"

	dp := NewDataPlane(cfg)
	tc := tlsGetterFromHTTPSListener(dp)
	if tc == nil {
		t.Fatal("HTTPS listener should still be created (process alive, B plan)")
	}
	if tc.GetCertificate == nil {
		t.Fatal("direct without source should install a reject-all GetCertificate, not leave it nil")
	}
	if len(tc.Certificates) != 0 {
		t.Fatalf("direct without source must not fall back to config static cert, got %d", len(tc.Certificates))
	}
	cert, err := tc.GetCertificate(&tls.ClientHelloInfo{ServerName: "any.test"})
	if err == nil {
		t.Fatal("reject-all getter must return an error (handshake rejected)")
	}
	if cert != nil {
		t.Fatal("reject-all getter must not return a certificate")
	}
}

// TestGlobalStillLoadsStaticCert: global 模式保持既有行为——config 静态证书照常装载，
// 不因 direct 改动而回归。
func TestGlobalStillLoadsStaticCert(t *testing.T) {
	// 生成一张临时自签证书写入文件，供 global 装载。
	certFile, keyFile := writeSelfSigned(t, "global.test")
	cfg := DataPlaneConfig{
		HTTPSAddress: ":10444",
		TLSMode:      TLSModeGlobal,
		CertFile:     certFile,
		KeyFile:      keyFile,
		Logger:       zap.NewNop(),
	}

	dp := NewDataPlane(cfg)
	tc := tlsGetterFromHTTPSListener(dp)
	if tc == nil {
		t.Fatal("expected an HTTPS tls.Config in global mode")
	}
	if len(tc.Certificates) != 1 {
		t.Fatalf("global mode should load exactly one static cert, got %d", len(tc.Certificates))
	}
}
