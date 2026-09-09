package certificate

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"
)

// genCert 生成覆盖 dnsNames 的自签证书，返回 (certPEM, keyPEM, leaf)。
func genCert(t *testing.T, dnsNames []string, notBefore, notAfter time.Time) (string, string, *x509.Certificate) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: dnsNames[0]},
		DNSNames:     dnsNames,
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return string(certPEM), string(keyPEM), leaf
}

// pemEncodeCertDER 把 DER 编码证书包成 PEM（测试用）。
func pemEncodeCertDER(der []byte) string {
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func TestValidateAndLoadOK(t *testing.T) {
	now := time.Now()
	certPEM, keyPEM, _ := genCert(t, []string{"shop-a.test"}, now.Add(-time.Hour), now.Add(24*time.Hour))
	pair, leaf, err := validateAndLoad(certPEM, keyPEM, "shop-a.test")
	if err != nil {
		t.Fatalf("validateAndLoad: %v", err)
	}
	if pair == nil || leaf == nil {
		t.Fatal("expected non-nil pair/leaf")
	}
}

func TestValidateAndLoadMismatchedKey(t *testing.T) {
	now := time.Now()
	_, _, _ = genCert(t, []string{"a.test"}, now, now.Add(time.Hour)) // other key
	certPEM, _, _ := genCert(t, []string{"a.test"}, now.Add(-time.Hour), now.Add(24*time.Hour))
	_, otherKey, _ := genCert(t, []string{"other.test"}, now.Add(-time.Hour), now.Add(24*time.Hour))
	if _, _, err := validateAndLoad(certPEM, otherKey, "a.test"); err == nil {
		t.Fatal("mismatched key must be rejected")
	}
}

func TestValidateAndLoadSANNotCovering(t *testing.T) {
	now := time.Now()
	certPEM, keyPEM, _ := genCert(t, []string{"shop-a.test"}, now.Add(-time.Hour), now.Add(24*time.Hour))
	if _, _, err := validateAndLoad(certPEM, keyPEM, "shop-b.test"); err == nil {
		t.Fatal("SAN not covering hostname must be rejected")
	}
}

func TestValidateAndLoadWildcardCoversSubdomain(t *testing.T) {
	now := time.Now()
	certPEM, keyPEM, _ := genCert(t, []string{"*.example.com"}, now.Add(-time.Hour), now.Add(24*time.Hour))
	if _, _, err := validateAndLoad(certPEM, keyPEM, "shop.example.com"); err != nil {
		t.Fatalf("wildcard should cover subdomain: %v", err)
	}
	if _, _, err := validateAndLoad(certPEM, keyPEM, "example.com"); err == nil {
		t.Fatal("wildcard *.example.com must not cover apex example.com")
	}
}

func TestValidateAndLoadExpired(t *testing.T) {
	now := time.Now()
	certPEM, keyPEM, _ := genCert(t, []string{"a.test"}, now.Add(-48*time.Hour), now.Add(-24*time.Hour))
	if _, _, err := validateAndLoad(certPEM, keyPEM, "a.test"); err == nil {
		t.Fatal("expired certificate must be rejected")
	}
}
