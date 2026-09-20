package certificate

import (
	"context"
	"sync/atomic"

	"github.com/NeoPlayful/maple-gateway/server/ent"
	"github.com/NeoPlayful/maple-gateway/server/internal/certificate/acme"
	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"go.uber.org/zap"
)

// ACMEConfig 是 ACME provider 的运行期配置（来自 settings.acme 分区，可热更）。
// Enabled=false 时 provider 仍注册但拒绝签发（配置语义：关闭时仅支持手动上传证书）。
type ACMEConfig struct {
	Enabled      bool
	DirectoryURL string
	Email        string
	Challenge    string
	KeyType      string
}

// acmeProvider 实现 CertificateProvider 的 ACME 来源：
// 证书材料由 ACME CA 签发（http-01 挑战由数据平面代答）。
// 只产出材料，不落库/不写缓存——编排由 Service 负责。
// 配置以原子指针持有：运行期改 directory_url/email/challenge/key_type 后，
// 下一次签发/续期即按新配置走，无需重启。
type acmeProvider struct {
	svc        *Service
	store      acme.AccountStore
	challenges *acme.ChallengeStore
	log        *zap.Logger
	cfg        atomic.Pointer[ACMEConfig]
}

func (p *acmeProvider) Name() string { return string(SourceACME) }

// SetConfig 原子替换运行期配置。
func (p *acmeProvider) SetConfig(c ACMEConfig) { p.cfg.Store(&c) }

func (p *acmeProvider) config() ACMEConfig {
	if c := p.cfg.Load(); c != nil {
		return *c
	}
	return ACMEConfig{}
}

// client 按当前配置构造 ACME 客户端（无状态，每次操作现构）。
func (p *acmeProvider) client() *acme.Client {
	c := p.config()
	return acme.NewClient(acme.Config{
		DirectoryURL: c.DirectoryURL,
		Email:        c.Email,
		Challenge:    c.Challenge,
		KeyType:      acme.KeyType(c.KeyType),
		AccountStore: p.store,
		Challenges:   p.challenges,
		Log:          p.log,
	})
}

// ensureEnabled 拦截关闭状态下的签发：配置语义为「关闭时仅支持手动上传证书」。
func (p *acmeProvider) ensureEnabled() error {
	if !p.config().Enabled {
		return pkg.ErrValidation("ACME 自动签发未启用，请先在系统设置中开启")
	}
	return nil
}

// Issue 申请新证书。
func (p *acmeProvider) Issue(ctx context.Context, req IssueRequest) (*Issued, error) {
	if err := p.ensureEnabled(); err != nil {
		return nil, err
	}
	out, err := p.client().Obtain(ctx, req.Hostname, acme.ProgressFunc(req.Progress))
	if err != nil {
		return nil, err
	}
	return toIssued(out), nil
}

// Renew 续期（ACME 语义上与首次签发相同：重新走一遍 order）。
func (p *acmeProvider) Renew(ctx context.Context, req RenewRequest) (*Issued, error) {
	if err := p.ensureEnabled(); err != nil {
		return nil, err
	}
	out, err := p.client().Obtain(ctx, req.Hostname, acme.ProgressFunc(req.Progress))
	if err != nil {
		return nil, err
	}
	return toIssued(out), nil
}

// Revoke 撤销：取本地证书 PEM，向 CA 提交撤销。
// 撤销不受 enabled 门控：存量证书仍需可撤销。
func (p *acmeProvider) Revoke(ctx context.Context, req RevokeRequest) error {
	rec, err := p.svc.repo.GetByID(ctx, req.CertificateID)
	if err != nil {
		return err
	}
	return p.client().Revoke(ctx, rec.CertificatePEM, req.Reason)
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
// cfg 为初始运行期配置，之后可经 SetACMEConfig 热更（不改注册表）。
func (s *Service) EnableACME(entClient *ent.Client, challenges *acme.ChallengeStore, cfg ACMEConfig) {
	store := &acmeAccountRepo{ent: entClient, enc: s.enc, log: s.log}
	p := &acmeProvider{svc: s, store: store, challenges: challenges, log: s.log}
	p.SetConfig(cfg)
	s.acme = p
	s.providers.Register(p)
	if s.log != nil {
		s.log.Info("acme provider registered",
			zap.Bool("enabled", cfg.Enabled),
			zap.String("directory", cfg.DirectoryURL),
			zap.String("challenge", cfg.Challenge),
			zap.String("key_type", cfg.KeyType))
	}
}

// SetACMEConfig 热更 ACME provider 运行期配置；未启用（provider 未注册）时静默忽略。
func (s *Service) SetACMEConfig(cfg ACMEConfig) {
	if s.acme != nil {
		s.acme.SetConfig(cfg)
	}
}
