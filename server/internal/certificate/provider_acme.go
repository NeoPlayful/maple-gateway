package certificate

import (
	"context"

	"github.com/NeoPlayful/maple-gateway/server/ent"
	"github.com/NeoPlayful/maple-gateway/server/internal/certificate/acme"
	"go.uber.org/zap"
)

// acmeProvider 实现 CertificateProvider 的 ACME 来源：
// 证书材料由 ACME CA 签发（http-01 挑战由数据平面代答）。
// 只产出材料，不落库/不写缓存——编排由 Service 负责。
type acmeProvider struct {
	svc    *Service
	client *acme.Client
}

func (p *acmeProvider) Name() string { return string(SourceACME) }

// Issue 申请新证书。
func (p *acmeProvider) Issue(ctx context.Context, req IssueRequest) (*Issued, error) {
	out, err := p.client.Obtain(ctx, req.Hostname, acme.ProgressFunc(req.Progress))
	if err != nil {
		return nil, err
	}
	return toIssued(out), nil
}

// Renew 续期（ACME 语义上与首次签发相同：重新走一遍 order）。
func (p *acmeProvider) Renew(ctx context.Context, req RenewRequest) (*Issued, error) {
	out, err := p.client.Obtain(ctx, req.Hostname, acme.ProgressFunc(req.Progress))
	if err != nil {
		return nil, err
	}
	return toIssued(out), nil
}

// Revoke 撤销：取本地证书 PEM，向 CA 提交撤销。
func (p *acmeProvider) Revoke(ctx context.Context, req RevokeRequest) error {
	rec, err := p.svc.repo.GetByID(ctx, req.CertificateID)
	if err != nil {
		return err
	}
	return p.client.Revoke(ctx, rec.CertificatePEM, req.Reason)
}

// Status 读本地记录（与 manual 一致：来源侧状态即本地证书状态）。
func (p *acmeProvider) Status(ctx context.Context, hostname string) (ProviderStatus, error) {
	rec, err := p.svc.repo.GetByHostname(ctx, hostname)
	if err != nil {
		return ProviderStatus{}, err
	}
	return ProviderStatus{
		State:     string(rec.Status),
		Detail:    rec.Issuer,
		LastError: rec.LastRenewError,
	}, nil
}

// toIssued 把 acme 产出映射为 provider 契约的 Issued（provider_meta 记 order/challenge）。
func toIssued(out *acme.Issued) *Issued {
	meta := map[string]string{}
	if out.OrderURL != "" {
		meta["order_url"] = out.OrderURL
	}
	if out.Challenge != "" {
		meta["challenge"] = out.Challenge
	}
	return &Issued{
		CertificatePEM: out.CertificatePEM,
		PrivateKeyPEM:  out.PrivateKeyPEM,
		NotAfter:       out.NotAfter,
		Issuer:         out.Issuer,
		SerialNumber:   out.SerialNumber,
		ProviderMeta:   meta,
	}
}

// EnableACME 构造 ACME provider 并注册到来源注册表。
// 需要 DB（账户持久化）与已就绪的加密器（Service 构造时已具备）；challenges 与数据平面共享。
func (s *Service) EnableACME(entClient *ent.Client, challenges *acme.ChallengeStore,
	directoryURL, email, challenge, keyType string) {
	store := &acmeAccountRepo{ent: entClient, enc: s.enc, log: s.log}
	cl := acme.NewClient(acme.Config{
		DirectoryURL: directoryURL,
		Email:        email,
		Challenge:    challenge,
		KeyType:      acme.KeyType(keyType),
		AccountStore: store,
		Challenges:   challenges,
		Log:          s.log,
	})
	s.providers.Register(&acmeProvider{svc: s, client: cl})
	if s.log != nil {
		s.log.Info("acme provider enabled",
			zap.String("directory", directoryURL),
			zap.String("challenge", challenge),
			zap.String("key_type", keyType))
	}
}
