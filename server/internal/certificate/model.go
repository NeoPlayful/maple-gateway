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
	// 续期引擎：来源侧上下文（脱敏，可落库/可展示）。
	ProviderMeta map[string]string `json:"provider_meta,omitempty"`
	// RenewAttempts 连续续期失败次数（退避用；成功后清零）。
	RenewAttempts int `json:"renew_attempts"`
	// NextRenewAt 下次续期时间；空则按 expires_at 推算。
	NextRenewAt *time.Time `json:"next_renew_at,omitempty"`
	// LastRenewError 最近一次续期失败原因。
	LastRenewError string    `json:"last_renew_error,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
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

// UpdateRequest 是"更换证书材料"的输入：按 id 定位记录，hostname 保持不变。
// 主要用于证书续期/替换——重新提交一份证书与私钥。
//
// DomainID 为三态语义：domain_id 非空=换绑到该域名；domain_id_clear=true=显式解绑；
// 二者皆否=保持原绑定（默认，避免只换证书时误解绑）。
// certificate_pem / private_key_pem 均必填。hostname 不在此结构，编辑时不可改。
type UpdateRequest struct {
	CertificatePEM string     `json:"certificate_pem" validate:"required"`
	PrivateKeyPEM  string     `json:"private_key_pem" validate:"required"`
	DomainID       *uuid.UUID `json:"domain_id,omitempty"`
	ClearDomainID  bool       `json:"domain_id_clear,omitempty"`
}

// Loaded 是内存缓存中可直接用于握手的对象。
type Loaded struct {
	Hostname string
	Cert     *tls.Certificate
	Expiry   time.Time
	// Leaf 便于 SAN / 有效期等只读检查（LoadX509KeyPair 已解析，缓存 leaf）。
	Leaf *x509.Certificate
}
