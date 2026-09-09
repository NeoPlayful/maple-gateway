package canary

import (
	"context"
	"time"

	"go.uber.org/zap"
)

// VersionStat 是自动 Canary 评估所需的单个版本指标快照。
type VersionStat struct {
	Requests int64
	Errors   int64
	// AvgMS 平均延迟（毫秒）。可选：低于 ErrLatencyMaxMS 判定时才用。
	AvgMS float64
}

// MetricReader 抽象版本指标读取，便于注入真实时间桶或单测替身。
// Report 返回某 deployment version 在最近评估窗口的指标；数据不足返回 ok=false。
type MetricReader interface {
	Report(ctx context.Context, versionID string) (VersionStat, bool)
}

// AutoConfig 是自动 Canary 执行器配置。
type AutoConfig struct {
	Enabled      bool          // 总开关
	Interval     time.Duration // 评估周期
	ErrRateMax   float64       // 错误率上限（%）。canary 超过即自动回滚
	ErrLatencyMS float64       // 平均延迟上限（ms），>0 才启用延迟判定
	MinRequests  int64         // 窗口内最小请求数，不足则本轮不推进（样本不足）
}

// Auto 是自动 Canary 执行器：周期扫描 running 发布，按版本错误率
// 健康则按 step 自动推进权重，异常则自动止损（权重归 0 + paused）。
//
// 人工 pause 后发布进入 paused，执行器不再推进；人工可随时接管。
// 注意：止损走 SetWeight(0)+Pause（保留 running 历史、可 resume 观察），
// 而非 Rollback 终态——彻底回滚仍由人工 promote/rollback 决策。
type Auto struct {
	svc    *Service
	repo   *Repository
	metric MetricReader
	cfg    AutoConfig
	logger *zap.Logger
}

// NewAuto 构造。metric 为空时执行器不启动（无数据源）。
func NewAuto(svc *Service, repo *Repository, metric MetricReader, logger *zap.Logger, cfg AutoConfig) *Auto {
	return &Auto{svc: svc, repo: repo, metric: metric, cfg: cfg, logger: logger}
}

// Run 启动评估循环，直到 ctx 取消。
func (a *Auto) Run(ctx context.Context) {
	if !a.cfg.Enabled || a.metric == nil {
		return
	}
	if a.cfg.Interval <= 0 {
		a.cfg.Interval = 15 * time.Second
	}
	if a.cfg.ErrRateMax <= 0 {
		a.cfg.ErrRateMax = 5 // 默认错误率 >5% 回滚
	}
	if a.cfg.MinRequests <= 0 {
		a.cfg.MinRequests = 10
	}
	t := time.NewTicker(a.cfg.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			a.evaluateOnce(ctx)
		}
	}
}

// evaluateOnce 扫描一轮 running 发布并执行自动推进/回滚。
func (a *Auto) evaluateOnce(ctx context.Context) {
	releases, _, err := a.repo.List(ctx, nil, PhaseRunning, 100, 0)
	if err != nil {
		if a.logger != nil {
			a.logger.Warn("auto canary list running failed", zap.String("err", err.Error()))
		}
		return
	}
	for _, rel := range releases {
		a.evaluateRelease(ctx, rel)
	}
}

// evaluateRelease 评估单个 running 发布。
// 决策：
//   - canary 样本不足 → 本轮不动（等待更多流量）。
//   - canary 错误率/延迟超阈值 → 自动止损：权重归 0 + paused（可 resume 观察）。
//   - 已达 TargetWeight → 不推进（等待人工 promote，或继续观察）。
//   - 健康且未达目标 → 按 step 推进。
func (a *Auto) evaluateRelease(ctx context.Context, rel *Release) {
	cs, ok := a.metric.Report(ctx, rel.CanaryVersionID.String())
	if !ok || cs.Requests < a.cfg.MinRequests {
		return // 样本不足：不动作
	}

	// 异常判定：错误率或延迟超阈值 → 自动止损（权重归 0 + 暂停），运营可 resume 观察。
	if rollback := a.shouldRollback(cs); rollback {
		if _, err := a.svc.SetWeight(ctx, rel.ID, 0); err != nil {
			if a.logger != nil {
				a.logger.Warn("auto canary stop-loss set weight failed",
					zap.String("release", rel.ID.String()), zap.String("err", err.Error()))
			}
			return
		}
		if _, err := a.svc.Pause(ctx, rel.ID); err != nil {
			if a.logger != nil {
				a.logger.Warn("auto canary stop-loss pause failed",
					zap.String("release", rel.ID.String()), zap.String("err", err.Error()))
			}
			return
		}
		if a.logger != nil {
			a.logger.Warn("auto canary stopped (weight 0, paused)",
				zap.String("release", rel.ID.String()),
				zap.Int64("requests", cs.Requests),
				zap.Int64("errors", cs.Errors))
		}
		return
	}

	// 已达目标权重：不推进（等待 promote），避免无限循环到 100 后仍步进。
	if rel.CanaryWeight >= rel.TargetWeight {
		return
	}

	// 健康且未达目标：按 step 推进。
	next := rel.CanaryWeight + rel.StepWeight
	if next > rel.TargetWeight {
		next = rel.TargetWeight
	}
	if next <= rel.CanaryWeight {
		return
	}
	if _, err := a.svc.SetWeight(ctx, rel.ID, next); err != nil {
		if a.logger != nil {
			a.logger.Warn("auto canary step failed",
				zap.String("release", rel.ID.String()), zap.String("err", err.Error()))
		}
		return
	}
	if a.logger != nil {
		a.logger.Info("auto canary stepped",
			zap.String("release", rel.ID.String()),
			zap.Int("from", rel.CanaryWeight),
			zap.Int("to", next))
	}
}

// shouldRollback 判定 canary 是否异常应回滚。延迟阈值仅在配置 >0 时参与。
func (a *Auto) shouldRollback(cs VersionStat) bool {
	rate := 0.0
	if cs.Requests > 0 {
		rate = float64(cs.Errors) / float64(cs.Requests) * 100
	}
	if rate > a.cfg.ErrRateMax {
		return true
	}
	if a.cfg.ErrLatencyMS > 0 && cs.AvgMS > a.cfg.ErrLatencyMS {
		return true
	}
	return false
}
