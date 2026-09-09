package certificate

import (
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"time"
)

// validateAndLoad 解析证书+私钥，校验两者匹配、未过期、可选 SAN 覆盖 hostname。
// 返回可直接用于握手的 tls.Certificate 与 leaf 证书。
func validateAndLoad(certPEM, keyPEM, hostname string) (*tls.Certificate, *x509.Certificate, error) {
	if strings.TrimSpace(certPEM) == "" {
		return nil, nil, errors.New("certificate PEM is empty")
	}
	if strings.TrimSpace(keyPEM) == "" {
		return nil, nil, errors.New("private key PEM is empty")
	}
	// PEM 可解析 + 证书/私钥匹配（tls.X509KeyPair 内部做两者配对）。
	pair, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		return nil, nil, fmt.Errorf("certificate/key mismatch or invalid PEM: %w", err)
	}
	// 公钥匹配复核（X509KeyPair 失败即是配对错误，此处再显式校验一次以给出精确错误）。
	leaf, err := parseLeaf(certPEM)
	if err != nil {
		return nil, nil, err
	}
	if !publicKeysEqual(leaf.PublicKey, pair.PrivateKey) {
		return nil, nil, errors.New("certificate public key does not match private key")
	}
	now := time.Now()
	if now.Before(leaf.NotBefore) {
		return nil, nil, fmt.Errorf("certificate not yet valid (notBefore %s)", leaf.NotBefore.Format(time.RFC3339))
	}
	if now.After(leaf.NotAfter) {
		return nil, nil, fmt.Errorf("certificate expired (notAfter %s)", leaf.NotAfter.Format(time.RFC3339))
	}
	if hostname != "" && !leafMatchesHostname(leaf, hostname) {
		return nil, nil, fmt.Errorf("certificate SAN does not cover hostname %q", hostname)
	}
	return &pair, leaf, nil
}

// parseLeaf 解析证书 PEM 并返回第一个 leaf。
func parseLeaf(certPEM string) (*x509.Certificate, error) {
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, errors.New("certificate PEM parse failed")
	}
	c, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse certificate: %w", err)
	}
	return c, nil
}

// publicKeysEqual 比较证书公钥与私钥派生公钥是否同一。
func publicKeysEqual(pub, priv any) bool {
	switch pk := pub.(type) {
	case *rsa.PublicKey:
		pr, ok := priv.(*rsa.PrivateKey)
		return ok && pk.N.Cmp(pr.N) == 0 && pk.E == pr.E
	case *ecdsa.PublicKey:
		pr, ok := priv.(*ecdsa.PrivateKey)
		return ok && pk.X.Cmp(pr.X) == 0 && pk.Y.Cmp(pr.Y) == 0
	default:
		return false
	}
}

// leafMatchesHostname 判断 leaf 证书 SAN 是否覆盖 hostname（精确或父域 wildcard）。
func leafMatchesHostname(leaf *x509.Certificate, hostname string) bool {
	hostname = strings.ToLower(strings.TrimSuffix(hostname, "."))
	for _, dns := range leaf.DNSNames {
		if dns == "" {
			continue
		}
		d := strings.ToLower(strings.TrimSuffix(dns, "."))
		if d == hostname {
			return true
		}
		// *.example.com 覆盖 a.example.com 及任意子域。
		if strings.HasPrefix(d, "*.") {
			base := d[2:]
			if strings.HasSuffix(hostname, "."+base) {
				return true
			}
		}
	}
	// 兼容 CN（弱校验兜底，正常以 SAN 为准）。
	if leaf.Subject.CommonName != "" && strings.EqualFold(strings.TrimSuffix(leaf.Subject.CommonName, "."), hostname) {
		return true
	}
	return false
}
