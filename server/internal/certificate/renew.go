package certificate

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/internal/certificate/acme"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// RenewConfig 是续期引擎可调参数（来自 config.ACMEConfig）。
type RenewConfig struct {
	Before      time.Duration // 提前续期窗口（如 720h）
	MaxAttempts int           // 连续失败上限，超过转手动告警
	RateBackoff time.Duration // 触发 CA 限流后的最小重试间隔
	BaseBackoff time.Duration // 指数退避基数
}

// CanRun 决定本实例本轮是否可执行续期。
// 多实例下由 main 传入 "仅 Leader 执行"（复用 HA 协调，避免重复下单）；
// 单实例（HA 关闭）传 nil，视为恒可运行。
type CanRun func() bool

// RenewOnce 扫描一轮临期证书并逐条执行续期，返回本轮成功数。
// 由 main 周期调用（renew_check_interval）。
func (s *Service) RenewOnce(ctx context.Context, cfg RenewConfig, canRun CanRun) (int, error) {
	if canRun != nil && !canRun() {
		// 非 Leader：本实例不执行（另一实例会处理），避免多实例重复向 CA 下单。
		return 0, nil
	}
	deadline := time.Now().Add(cfg.Before)
	due, err := s.repo.DueForRenewal(ctx, deadline)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, rec := range due {
		if err := s.renewOne(ctx, rec, cfg); err == nil {
			n++
		}
	}
	return n, nil
}

// RenewNow 立即续期指定证书（管理员触发，跳过临期窗口判断）。
func (s *Service) RenewNow(ctx context.Context, id uuid.UUID) (*Certificate, error) {
	rec, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	cfg := RenewConfig{Before: 30 * 24 * time.Hour, MaxAttempts: 5, BaseBackoff: time.Hour}
	if err := s.renewOne(ctx, rec, cfg); err != nil {
		return nil, mapProviderErr(err)
	}
	return s.repo.GetByID(ctx, id)
}

// renewOne 续期单条证书。失败走退避记录，成功原子换证。
func (s *Service) renewOne(ctx context.Context, rec *Certificate, cfg RenewConfig) error {
	p, err := s.providers.For(rec.Source)
	if err != nil {
		// 来源不支持续期（如 manual 未注册 renew）；跳过不记失败。
		return err
	}
	issued, err := p.Renew(ctx, RenewRequest{CertificateID: rec.ID, Hostname: rec.Hostname})
	if err != nil {
		if errors.Is(err, ErrNotSupported) {
			return err
		}
		s.onRenewFailure(ctx, rec, cfg, err)
		return err
	}
	_, leaf, verr := validateAndLoad(issued.CertificatePEM, issued.PrivateKeyPEM, rec.Hostname)
	if verr != nil {
		s.onRenewFailure(ctx, rec, cfg, verr)
		return verr
	}
	encKey, eerr := s.enc.Encrypt([]byte(issued.PrivateKeyPEM))
	if eerr != nil {
		s.onRenewFailure(ctx, rec, cfg, eerr)
		return eerr
	}
	next := issued.NotAfter.Add(-cfg.Before)
	in := &Certificate{
		CertificatePEM:      issued.CertificatePEM,
		PrivateKeyEncrypted: encKey,
		Source:              rec.Source,
		Issuer:              issued.Issuer,
		SerialNumber:        issued.SerialNumber,
		IssuedAt:            &leaf.NotBefore,
		ExpiresAt:           &issued.NotAfter,
		ProviderMeta:        issued.ProviderMeta,
		NextRenewAt:         &next,
	}
	saved, err := s.repo.RenewSuccess(ctx, rec.ID, in)
	if err != nil {
		s.log.Error("certificate renew persist failed",
			zap.String("hostname", rec.Hostname), zap.Error(err))
		return err
	}
	// 原子刷新缓存：新连接即用新证，在途连接不受影响。
	if err := s.reloadIntoCache(saved); err != nil {
		s.log.Warn("certificate cache reload after renew failed",
			zap.String("hostname", rec.Hostname), zap.Error(err))
	}
	s.syncDomainTLS(ctx, rec.Hostname, "managed", saved.Status)
	s.log.Info("certificate renewed",
		zap.String("hostname", rec.Hostname),
		zap.Time("expires_at", issued.NotAfter),
		zap.Time("next_renew_at", next))
	return nil
}

// onRenewFailure 记录续期失败：累加 attempts、指数退避设置 next_renew_at、写 last_renew_error。
// 达上限后停止自动重试（next_renew_at 推远）并高优先级告警。
func (s *Service) onRenewFailure(ctx context.Context, rec *Certificate, cfg RenewConfig, cause error) {
	attempts := rec.RenewAttempts + 1
	d := backoffDuration(cfg, attempts, cause)
	next := time.Now().Add(d)
	msg := cause.Error()
	if cfg.MaxAttempts > 0 && attempts >= cfg.MaxAttempts {
		msg = fmt.Sprintf("%s (已达重试上限 %d，停止自动重试，转人工处理)", msg, cfg.MaxAttempts)
		// 推到远期，避免继续高频重试；人工修复后重新签发会清零。
	}
	if _, err := s.repo.RenewFailure(ctx, rec.ID, attempts, &next, msg); err != nil {
		s.log.Warn("record certificate renewal failure failed",
			zap.String("hostname", rec.Hostname), zap.Error(err))
	}
	s.log.Warn("certificate renewal failed",
		zap.String("hostname", rec.Hostname),
		zap.Int("attempts", attempts),
		zap.Duration("next_retry_in", d),
		zap.Error(cause))
}

// backoffDuration 计算退避间隔：基数指数增长（封顶 24h）；限流时不低于 RateBackoff。
func backoffDuration(cfg RenewConfig, attempts int, cause error) time.Duration {
	base := cfg.BaseBackoff
	if base <= 0 {
		base = time.Hour
	}
	d := base
	for i := 1; i < attempts; i++ {
		if d >= 24*time.Hour {
			break
		}
		d *= 2
	}
	if d > 24*time.Hour {
		d = 24 * time.Hour
	}
	if acme.IsRateLimited(cause) && cfg.RateBackoff > d {
		d = cfg.RateBackoff
	}
	return d
}
