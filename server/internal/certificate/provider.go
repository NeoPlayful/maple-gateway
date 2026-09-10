package certificate

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// ErrNotSupported 表示 provider 不支持该操作（如 Manual 来源不能主动签发/续期）。
var ErrNotSupported = errors.New("certificate provider: operation not supported")

// ErrProviderUnavailable 表示请求的来源未注册/未启用（如 cloudflare 本期不接）。
var ErrProviderUnavailable = errors.New("certificate provider unavailable")

// ChallengeType 是 ACME 域名验证方式。
type ChallengeType string

const (
	ChallengeHTTP01    ChallengeType = "http-01"
	ChallengeTLSALPN01 ChallengeType = "tls-alpn-01"
)

// ProgressFunc 在签发/续期流程的阶段转换处被回调：step 为阶段标识（与 OperationStatus 对齐），
// msg 为人类可读说明。可空；实现不得阻塞（上层仅做一次轻量写库）。
type ProgressFunc func(step, msg string)

// IssueRequest 是申请新证书的输入。
type IssueRequest struct {
	Hostname  string
	DomainID  *uuid.UUID
	Challenge ChallengeType
	// Progress 可空：ACME 流程的阶段进度回调，manual 来源忽略。
	Progress ProgressFunc
}

// RenewRequest 是续期输入。
type RenewRequest struct {
	CertificateID uuid.UUID
	Hostname      string
	// Progress 可空：ACME 流程的阶段进度回调，manual 来源忽略。
	Progress ProgressFunc
}

// RevokeRequest 是撤销输入。Reason 为 RFC 5280 撤销原因码（0=unspecified）。
type RevokeRequest struct {
	CertificateID uuid.UUID
	Hostname      string
	Reason        int
}

// Issued 是 provider 产出的证书材料。PrivateKeyPEM 仅存在于内存流转，
// 由 Service 在落库前加密；任何输出模型都不得包含它。
type Issued struct {
	CertificatePEM string
	PrivateKeyPEM  string
	ChainPEM       string
	NotAfter       time.Time
	Issuer         string
	SerialNumber   string
	// ProviderMeta 是来源侧上下文（如 ACME order URL / challenge 类型），已脱敏，可落库。
	ProviderMeta map[string]string
}

// ProviderStatus 是来源侧的证书状态视图。
type ProviderStatus struct {
	State     string
	Detail    string
	LastError string
}

// CertificateProvider 描述"如何取得证书材料"的统一契约。
//
// 实现只负责与来源交互（人工上传 / ACME 签发 / CA 撤销），**不得**写库、写缓存或写审计——
// 落库、私钥加密、缓存刷新、审计全部由 Service 编排，保证 provider 可插拔而副作用集中。
type CertificateProvider interface {
	// Name 返回来源标识（"manual" / "acme"），与 Source 枚举对齐。
	Name() string
	// Issue 申请新证书。不支持的来源返回 ErrNotSupported。
	Issue(ctx context.Context, req IssueRequest) (*Issued, error)
	// Renew 续期。不支持的来源返回 ErrNotSupported。
	Renew(ctx context.Context, req RenewRequest) (*Issued, error)
	// Revoke 撤销。不支持的来源返回 ErrNotSupported。
	Revoke(ctx context.Context, req RevokeRequest) error
	// Status 返回来源侧状态。
	Status(ctx context.Context, hostname string) (ProviderStatus, error)
}
