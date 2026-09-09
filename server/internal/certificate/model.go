// Package certificate 实现 Direct TLS 证书管理：
// 校验、加密存储、内存缓存、动态 SNI。
// 握手热路径只读缓存，严禁在 GetCertificate 内访问 DB。
package certificate

import (
	"crypto/tls"
	"crypto/x509"
	"time"

	"github.com/google/uuid"
)

// Source 证书来源。
type Source string

const (
	SourceManual     Source = "manual"
	SourceCloudflare Source = "cloudflare"
	SourceACME       Source = "acme"
	SourceOrigin     Source = "origin"
)

// Status 证书状态（与 Domain 路由状态分离）。
type Status string

const (
	StatusPending  Status = "pending"
	StatusActive   Status = "active"
	StatusError    Status = "error"
	StatusExpiring Status = "expiring"
	StatusExpired  Status = "expired"
)

// Certificate 是证书领域模型。privateKey 密文永不出现在 JSON / API。
type Certificate struct {
	ID                  uuid.UUID  `json:"id"`
	DomainID            *uuid.UUID `json:"domain_id,omitempty"`
	Hostname            string     `json:"hostname"`
	Source              Source     `json:"source"`
	Status              Status     `json:"status"`
	CertificatePEM      string     `json:"certificate_pem,omitempty"`
	PrivateKeyEncrypted string     `json:"-"`
	Issuer              string     `json:"issuer,omitempty"`
	SerialNumber        string     `json:"serial_number,omitempty"`
	IssuedAt            *time.Time `json:"issued_at,omitempty"`
	ExpiresAt           *time.Time `json:"expires_at,omitempty"`
	LastRenewedAt       *time.Time `json:"last_renewed_at,omitempty"`
	LastError           string     `json:"last_error,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

// New 上传证书输入。private_key_pem 仅存在于输入，落库前加密，永不出现在任何输出。
type New struct {
	DomainID       *uuid.UUID `json:"domain_id,omitempty"`
	Hostname       string     `json:"hostname" validate:"required"`
	CertificatePEM string     `json:"certificate_pem" validate:"required"`
	PrivateKeyPEM  string     `json:"private_key_pem" validate:"required"`
}

// Update 可修改字段。
type Update struct {
	Status    *Status `json:"status"`
	LastError *string `json:"last_error"`
}

// Loaded 是内存缓存中可直接用于握手的对象。
type Loaded struct {
	Hostname string
	Cert     *tls.Certificate
	Expiry   time.Time
	// Leaf 便于 SAN / 有效期等只读检查（LoadX509KeyPair 已解析，缓存 leaf）。
	Leaf *x509.Certificate
}
