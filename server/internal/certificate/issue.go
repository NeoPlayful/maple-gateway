package certificate

import (
	"context"
	"errors"

	"github.com/NeoPlayful/maple-gateway/server/internal/router"
	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Issue 同步签发某主机（Managed / ACME 来源）。供未启用异步运行时的精简部署/测试使用；
// 管理面默认走 IssueAsync（带进度可视化）。
func (s *Service) Issue(ctx context.Context, hostname string, domainID *uuid.UUID, source Source, challenge ChallengeType) (*Certificate, error) {
	rec, err := s.issueCore(ctx, hostname, domainID, source, challenge, nil)
	if err != nil {
		return nil, mapProviderErr(err)
	}
	return rec, nil
}

// issueCore 是签发核心：校验 hostname → 选 provider → 签发 → 落库/缓存 → 联动 Domain。
// 返回**未映射的原始错误**，便于异步 job 按类型分类（限流/挑战失败/账户无效）。
// progress 可空：透传给 provider 的阶段进度回调。
func (s *Service) issueCore(ctx context.Context, hostname string, domainID *uuid.UUID, source Source, challenge ChallengeType, progress ProgressFunc) (*Certificate, error) {
	h, err := router.NormalizeHost(hostname)
	if err != nil {
		return nil, pkg.ErrValidation("域名格式无效")
	}
	p, err := s.providers.For(source)
	if err != nil {
		// 来源未注册：多为该来源未在配置中启用（如 acme.enabled=false）。
		return nil, pkg.ErrValidation("证书来源未启用: " + string(source))
	}
	issued, err := p.Issue(ctx, IssueRequest{Hostname: h, DomainID: domainID, Challenge: challenge, Progress: progress})
	if err != nil {
		return nil, err
	}
	return s.persistIssued(ctx, h, domainID, source, issued)
}

// persistIssued 把 provider 产出落库并刷新缓存：校验 → 加密私钥 → upsert → 热加载 → 联动 Domain。
// 与 Upload 同语义：ACME 下发的证书同样过 validateAndLoad（SAN 必须覆盖 hostname）。
func (s *Service) persistIssued(ctx context.Context, hostname string, domainID *uuid.UUID, source Source, issued *Issued) (*Certificate, error) {
	_, leaf, err := validateAndLoad(issued.CertificatePEM, issued.PrivateKeyPEM, hostname)
	if err != nil {
		return nil, pkg.ErrValidation(err.Error())
	}
	// 解析绑定域名：显式 domain_id 优先并校验一致，否则按 hostname 反查。
	resolvedID, err := s.resolveDomainID(ctx, hostname, domainID)
	if err != nil {
		return nil, err
	}
	rec := &Certificate{
		DomainID:       resolvedID,
		Hostname:       hostname,
		Source:         source,
		Status:         StatusActive,
		CertificatePEM: issued.CertificatePEM,
		Issuer:         issued.Issuer,
		SerialNumber:   issued.SerialNumber,
		IssuedAt:       &leaf.NotBefore,
		ExpiresAt:      &issued.NotAfter,
		ProviderMeta:   issued.ProviderMeta,
	}
	encKey, err := s.enc.Encrypt([]byte(issued.PrivateKeyPEM))
	if err != nil {
		s.log.Error("certificate private key encrypt failed", zap.Error(err))
		return nil, pkg.ErrSystem("私钥加密失败")
	}
	rec.PrivateKeyEncrypted = encKey

	var saved *Certificate
	existing, err := s.repo.GetByHostname(ctx, hostname)
	switch {
	case err == nil:
		saved, err = s.repo.UpdateContent(ctx, existing.ID, rec)
		if err != nil {
			return nil, pkg.ErrSystem("证书更新失败")
		}
	case pkg.ErrCode(err) == pkg.CodeNotFound:
		saved, err = s.repo.Create(ctx, rec)
		if err != nil {
			return nil, err
		}
	default:
		return nil, err
	}
	if err := s.reloadIntoCache(saved); err != nil {
		s.log.Warn("certificate cache reload failed",
			zap.String("hostname", hostname), zap.Error(err))
	}
	mode := "managed"
	if source == SourceManual {
		mode = "manual"
	}
	s.syncDomainTLS(ctx, resolvedID, mode, saved.Status)
	return saved, nil
}

// mapProviderErr 把 provider 错误映射为业务错误码（不泄露内部细节）。
// 已是业务错误（校验/未启用等 AppError）原样返回，避免被误包成系统错误。
func mapProviderErr(err error) error {
	var ae *pkg.AppError
	if errors.As(err, &ae) {
		return ae
	}
	switch {
	case errors.Is(err, ErrProviderUnavailable):
		return pkg.ErrValidation("证书来源不可用")
	case errors.Is(err, ErrNotSupported):
		return pkg.ErrValidation("该证书来源不支持此操作")
	default:
		return pkg.ErrSystem(err.Error())
	}
}
