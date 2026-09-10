package certificate

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/internal/certificate/certenc"
	"github.com/NeoPlayful/maple-gateway/server/internal/domain"
	"github.com/NeoPlayful/maple-gateway/server/internal/router"
	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// encIface 加解密抽象（certenc 实现，便于测试注入）。
type encIface interface {
	Encrypt(plaintext []byte) (string, error)
	Decrypt(encoded string) ([]byte, error)
}

// repoIface 证书仓储抽象（*Repository 实现，测试可注入内存 fake）。
type repoIface interface {
	All(ctx context.Context) ([]*Certificate, error)
	Create(ctx context.Context, in *Certificate) (*Certificate, error)
	GetByID(ctx context.Context, id uuid.UUID) (*Certificate, error)
	GetByHostname(ctx context.Context, hostname string) (*Certificate, error)
	List(ctx context.Context, limit, offset int) ([]*Certificate, int, error)
	UpdateContent(ctx context.Context, id uuid.UUID, in *Certificate) (*Certificate, error)
	Delete(ctx context.Context, id uuid.UUID) error
	ActiveByExpiryBefore(ctx context.Context, deadline time.Time) ([]*Certificate, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, status Status, lastErr string) (*Certificate, error)
}

// Service 编排证书业务：上传校验 → 加密 → 落库 → 刷新缓存。
type Service struct {
	repo       repoIface
	cache      *Cache
	enc        encIface
	log        *zap.Logger
	encKey     string            // 显式配置的加密密钥（config 提供）；空则回退 env
	domainRepo *domain.Repository // 可空：联动 Domain TLS 状态
}

// ServiceOption 供可选依赖注入。
type ServiceOption func(*Service)

// WithDomainSync 注入 Domain 仓库，使证书上传/删除联动 domains.tls_mode / certificate_status。
func WithDomainSync(repo *domain.Repository) ServiceOption {
	return func(s *Service) { s.domainRepo = repo }
}

// WithEncKey 显式指定私钥加密密钥（base64 32 字节）。优先级高于环境变量
// MAPLE_CERT_ENC_KEY；不传时 NewService 回退读环境变量。
func WithEncKey(key string) ServiceOption {
	return func(s *Service) { s.encKey = key }
}

// NewService 构造。密钥缺失时返回 ErrNoKey（Direct TLS 证书存储不可降级为明文）。
func NewService(repo repoIface, cache *Cache, log *zap.Logger, opts ...ServiceOption) (*Service, error) {
	s := &Service{repo: repo, cache: cache, log: log}
	for _, o := range opts {
		o(s)
	}
	enc, err := newEncrypter(s.encKey)
	if err != nil {
		return nil, err
	}
	s.enc = enc
	return s, nil
}

// newEncrypter 按显式密钥优先、环境变量兜底构造加解密器。
func newEncrypter(configured string) (encIface, error) {
	if configured != "" {
		return certenc.NewFromEnv(configured)
	}
	return certenc.New()
}

// newServiceWithDeps 供测试注入 enc 实现（绕过 MAPLE_CERT_ENC_KEY 依赖）。
func newServiceWithDeps(repo repoIface, cache *Cache, enc encIface, log *zap.Logger) *Service {
	return &Service{repo: repo, cache: cache, enc: enc, log: log}
}

// Upload 上传/替换某域名的 Manual Certificate。
// 校验通过 → 私钥加密落库 → 更新内存缓存 → 返回不含私钥的模型。
func (s *Service) Upload(ctx context.Context, in New) (*Certificate, error) {
	hostname, err := router.NormalizeHost(in.Hostname)
	if err != nil {
		return nil, pkg.ErrValidation("域名格式无效")
	}
	// 校验 PEM 可解析、证书/私钥匹配、未过期、SAN 覆盖 hostname。
	if _, leaf, err := validateAndLoad(in.CertificatePEM, in.PrivateKeyPEM, hostname); err != nil {
		return nil, pkg.ErrValidation(err.Error())
	} else {
		rec := &Certificate{
			DomainID:       in.DomainID,
			Hostname:       hostname,
			Source:         SourceManual,
			Status:         StatusActive,
			CertificatePEM: in.CertificatePEM,
			Issuer:         leaf.Issuer.String(),
			SerialNumber:   leaf.SerialNumber.String(),
			IssuedAt:       &leaf.NotBefore,
			ExpiresAt:      &leaf.NotAfter,
		}
		encKey, err := s.enc.Encrypt([]byte(in.PrivateKeyPEM))
		if err != nil {
			s.log.Error("certificate private key encrypt failed", zap.Error(err))
			return nil, pkg.ErrSystem("私钥加密失败")
		}
		rec.PrivateKeyEncrypted = encKey

		// 同 hostname 已有证书 → 覆盖内容保留记录；否则新建。
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

		// 刷新内存缓存（热加载）：解密私钥后重建 tls.Certificate。
		if err := s.reloadIntoCache(saved); err != nil {
			s.log.Warn("certificate cache reload failed",
				zap.String("hostname", hostname), zap.Error(err))
		}
		// 联动 Domain：tls_mode=manual、certificate_status 反映证书可用。
		s.syncDomainTLS(ctx, hostname, true, saved.Status)
		return saved, nil
	}
}

// Update 更换指定证书的材料（续期/替换）。按 id 定位，hostname 保持不变；
// 重新校验 PEM、加密私钥、解析派生字段（issuer/serial/有效期）落库，并热加载缓存。
// domain 绑定为三态：in.DomainID 非空→换绑；in.ClearDomainID→解绑；皆否→保持原绑定。
func (s *Service) Update(ctx context.Context, id uuid.UUID, in UpdateRequest) (*Certificate, error) {
	// 先取原记录：hostname 与既定 domain 绑定由它决定，且不存在时提前 NotFound。
	old, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	hostname := old.Hostname
	// 校验 PEM 可解析、证书/私钥匹配、未过期、SAN 覆盖 hostname。
	_, leaf, err := validateAndLoad(in.CertificatePEM, in.PrivateKeyPEM, hostname)
	if err != nil {
		return nil, pkg.ErrValidation(err.Error())
	}
	// domain 三态：未指定则沿用原绑定。
	domainID := old.DomainID
	switch {
	case in.DomainID != nil:
		domainID = in.DomainID
	case in.ClearDomainID:
		domainID = nil
	}
	rec := &Certificate{
		DomainID:       domainID,
		Hostname:       hostname,
		Source:         SourceManual,
		Status:         StatusActive,
		CertificatePEM: in.CertificatePEM,
		Issuer:         leaf.Issuer.String(),
		SerialNumber:   leaf.SerialNumber.String(),
		IssuedAt:       &leaf.NotBefore,
		ExpiresAt:      &leaf.NotAfter,
	}
	encKey, err := s.enc.Encrypt([]byte(in.PrivateKeyPEM))
	if err != nil {
		s.log.Error("certificate private key encrypt failed", zap.Error(err))
		return nil, pkg.ErrSystem("私钥加密失败")
	}
	rec.PrivateKeyEncrypted = encKey

	saved, err := s.repo.UpdateContent(ctx, id, rec)
	if err != nil {
		if pkg.ErrCode(err) == pkg.CodeNotFound {
			return nil, err
		}
		return nil, pkg.ErrSystem("证书更新失败")
	}
	// 刷新内存缓存（热加载）：解密私钥后重建 tls.Certificate。
	if err := s.reloadIntoCache(saved); err != nil {
		s.log.Warn("certificate cache reload failed",
			zap.String("hostname", hostname), zap.Error(err))
	}
	// 联动 Domain：tls_mode=manual、certificate_status 反映证书可用。
	s.syncDomainTLS(ctx, hostname, true, saved.Status)
	return saved, nil
}

// syncDomainTLS 更新 hostname 对应 Domain 的 TLS 状态（可空 repo 时静默跳过）。
func (s *Service) syncDomainTLS(ctx context.Context, hostname string, manual bool, certStatus Status) {
	if s.domainRepo == nil {
		return
	}
	d, err := s.domainRepo.GetByHostname(ctx, hostname)
	if err != nil {
		// hostname 可能尚无 Domain 记录（裸证书管理）；不阻断证书本身。
		s.log.Debug("certificate uploaded without matching domain",
			zap.String("hostname", hostname))
		return
	}
	tlsMode := "disabled"
	if manual {
		tlsMode = "manual"
	}
	cs := string(certStatus)
	if _, err := s.domainRepo.Update(ctx, d.ID, domain.Update{
		TLSMode:           &tlsMode,
		CertificateStatus: &cs,
	}); err != nil {
		s.log.Warn("sync domain tls state failed",
			zap.String("hostname", hostname), zap.Error(err))
	}
}

// Reload 单条重载缓存（管理员显式 reload）。
func (s *Service) Reload(ctx context.Context, id uuid.UUID) (*Certificate, error) {
	rec, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.reloadIntoCache(rec); err != nil {
		s.log.Warn("certificate reload into cache failed",
			zap.String("id", rec.ID.String()), zap.Error(err))
		return rec, pkg.ErrSystem("证书缓存重载失败")
	}
	return rec, nil
}

// ReloadAll 全量重载缓存（启动 / 周期对账用），原子整体替换。
// 准入只看证书内容能否装载成功（密钥可解密、PEM 可组装）；status 是告警维度，
// expiring/expired 一律继续服务，避免 5s 对账把临期/已过期证书踢出缓存。
func (s *Service) ReloadAll(ctx context.Context) error {
	recs, err := s.repo.All(ctx)
	if err != nil {
		return err
	}
	next := make(map[string]*Loaded, len(recs))
	for _, r := range recs {
		loaded, err := s.decryptAndLoad(r)
		if err != nil {
			s.log.Warn("certificate skipped at reload",
				zap.String("hostname", r.Hostname), zap.Error(err))
			continue
		}
		next[r.Hostname] = loaded
	}
	s.cache.ReplaceAll(next)
	return nil
}

// Delete 删除证书并从缓存摘除；如该 hostname 已无证书则回落 disabled。
func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	rec, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	hostname := rec.Hostname
	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}
	s.cache.Delete(hostname)
	// 仍有其他证书则该域名保留 manual，否则回落 disabled。
	if s.domainRepo != nil {
		if _, lerr := s.repo.GetByHostname(ctx, hostname); lerr != nil {
			s.syncDomainTLS(ctx, hostname, false, "")
		}
	}
	return nil
}

// Get 详情。
func (s *Service) Get(ctx context.Context, id uuid.UUID) (*Certificate, error) {
	return s.repo.GetByID(ctx, id)
}

// List 列表。
func (s *Service) List(ctx context.Context, limit, offset int) ([]*Certificate, int, error) {
	return s.repo.List(ctx, limit, offset)
}

// ScanExpiring 更新证书到期状态（告警维度）：warnWindow 内到期置 expiring，已过期置 expired。
// 只改 DB 状态用于展示/提醒，绝不摘缓存——证书服务以"内容可装载"为准，
// 过期继续服务直到运维替换/删除，否则此处摘除会与 5s ReloadAll 全量重载形成拉锯。
func (s *Service) ScanExpiring(ctx context.Context, warnWindow time.Duration) (int, error) {
	deadline := time.Now().Add(warnWindow)
	recs, err := s.repo.ActiveByExpiryBefore(ctx, deadline)
	if err != nil {
		return 0, err
	}
	now := time.Now()
	count := 0
	for _, r := range recs {
		if r.ExpiresAt == nil {
			continue
		}
		if now.After(*r.ExpiresAt) {
			if _, err := s.repo.UpdateStatus(ctx, r.ID, StatusExpired, "证书已过期"); err == nil {
				count++
			}
			continue
		}
		if _, err := s.repo.UpdateStatus(ctx, r.ID, StatusExpiring, "证书即将过期"); err == nil {
			count++
		}
	}
	return count, nil
}

// Cache 暴露只读句柄供数据面 GetCertificate 使用。
func (s *Service) Cache() *Cache { return s.cache }

// reloadIntoCache 单条解密并装载缓存（上传热加载 / 手动 reload）。
// 与 ReloadAll 同语义：不按 status 拦截，仅内容可装载即入缓存。
func (s *Service) reloadIntoCache(rec *Certificate) error {
	loaded, err := s.decryptAndLoad(rec)
	if err != nil {
		return err
	}
	s.cache.Set(rec.Hostname, loaded)
	return nil
}

// decryptAndLoad 解密私钥并组装 tls.Certificate 缓存项。
func (s *Service) decryptAndLoad(rec *Certificate) (*Loaded, error) {
	keyPEM, err := s.enc.Decrypt(rec.PrivateKeyEncrypted)
	if err != nil {
		return nil, fmt.Errorf("decrypt private key: %w", err)
	}
	pair, err := tls.X509KeyPair([]byte(rec.CertificatePEM), keyPEM)
	if err != nil {
		return nil, fmt.Errorf("assemble certificate: %w", err)
	}
	leaf, err := parseLeaf(rec.CertificatePEM)
	if err != nil {
		return nil, err
	}
	return &Loaded{Hostname: rec.Hostname, Cert: &pair, Leaf: leaf, Expiry: leaf.NotAfter}, nil
}
