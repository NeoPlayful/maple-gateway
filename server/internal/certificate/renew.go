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

// RenewConfig 是续期引擎可调参数（来自 settings.acme 分区，可热更）。
type RenewConfig struct {
	Before      time.Duration // 提前续期窗口（如 720h）
	MaxAttempts int           // 连续失败上限，超过转手动告警
	RateBackoff time.Duration // 触发 CA 限流后的最小重试间隔
	BaseBackoff time.Duration // 指数退避基数
}

// DefaultRenewConfig 返回续期参数内建默认（对齐 config.Default.ACME）。
func DefaultRenewConfig() RenewConfig {
	return RenewConfig{
		Before:      720 * time.Hour,
		MaxAttempts: 5,
		RateBackoff: 6 * time.Hour,
		BaseBackoff: time.Hour,
	}
}

// CanRun 决定本实例本轮是否可执行续期。
// 多实例下由 main 传入 "仅 Leader 执行"（复用 HA 协调，避免重复下单）；
// 单实例（HA 关闭）传 nil，视为恒可运行。
type CanRun func() bool

// renewCfg 持有当前续期参数，供热更（RenewNow 与循环共用同一份）。
func (s *Service) renewConfig() RenewConfig {
	if c := s.renewCfg.Load(); c != nil {
		return *c
	}
	return DefaultRenewConfig()
}

// SetRenewConfig 热更续期参数；下一次 RenewNow/循环即生效。
func (s *Service) SetRenewConfig(cfg RenewConfig) { s.renewCfg.Store(&cfg) }

// acmeEnabled 报告 ACME 自动签发当前是否启用（provider 未注册视为未启用）。
func (s *Service) acmeEnabled() bool {
	return s.acme != nil && s.acme.config().Enabled
}

// RenewOnce 扫描一轮临期证书并逐条执行续期，返回本轮成功数。
// 由 main 周期调用（renew_check_interval）。cfg 为当轮参数（由调用方从 settings 读取）。
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
// 读当前热更参数（与自动循环同一份），使 rate_limit_backoff 等对人工续期同样生效。
func (s *Service) RenewNow(ctx context.Context, id uuid.UUID) (*Certificate, error) {
	rec, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.renewOne(ctx, rec, s.renewConfig()); err != nil {
		return nil, mapProviderErr(err)
	}
	return s.repo.GetByID(ctx, id)
}

// RenewLoop 是 ACME 自动续期循环：按 interval 周期扫描临期证书并续期。
// interval 与参数均可热更——SetRenewConfig 更新参数，SetRenewInterval 触发唤醒。
// 仅 acme==nil（未启用自动签发）时随 ctx 取消退出；enabled 关闭时循环仍在，
// 但 provider 的 ensureEnabled 会拒绝签发（不产生 CA 请求），管理员手动签发同理。
func (s *Service) RenewLoop(ctx context.Context, interval time.Duration, canRun CanRun) {
	if interval <= 0 {
		interval = time.Hour
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.renewWake:
			// 间隔热更：仅在变化时 Reset，避免频繁重建 ticker。
			if cur := s.renewNanos.Load(); cur > 0 {
				ticker.Reset(time.Duration(cur))
			}
		case <-ticker.C:
			// 关闭时不扫描：避免把「未启用」误记为续期失败（累加 attempts / 推远 next_renew_at）。
			if !s.acmeEnabled() {
				continue
			}
			cfg := s.renewConfig()
			if n, err := s.RenewOnce(ctx, cfg, canRun); err != nil {
				if s.log != nil {
					s.log.Warn("certificate auto-renew scan failed", zap.Error(err))
				}
			} else if n > 0 && s.log != nil {
				s.log.Info("certificate auto-renew completed", zap.Int("renewed", n))
			}
		}
	}
}

// SetRenewInterval 热更续期扫描间隔（秒级粒度），并唤醒循环即时应用。
func (s *Service) SetRenewInterval(interval time.Duration) {
	if interval <= 0 {
		return
	}
	s.renewNanos.Store(int64(interval))
	select {
	case s.renewWake <- struct{}{}:
	default:
		// 已有待处理唤醒信号，丢弃本次（循环会读到最新值）。
	}
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
	s.syncDomainTLS(ctx, rec.DomainID, "managed", saved.Status)
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
