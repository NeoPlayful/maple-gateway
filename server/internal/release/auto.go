package release

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
	ErrRateMax   float64       // 错误率上限（%）。canary 超过即自动止损
	ErrLatencyMS float64       // 平均延迟上限（ms），>0 才启用延迟判定
	MinRequests  int64         // 窗口内最小请求数，不足则本轮不推进（样本不足）
}

// Auto 是自动 Canary 执行器：周期扫描 running 的 canary 发布，按版本错误率
// 健康则按 step 自动推进权重，异常则自动止损（权重归 0 + paused）。
// 仅对 strategy=canary 生效；bluegreen 是二元配置，不参与自动推进。
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
		a.cfg.ErrRateMax = 5
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

// evaluateOnce 扫描一轮 running 的 canary 发布并执行自动推进/止损。
func (a *Auto) evaluateOnce(ctx context.Context) {
	strategy := StrategyCanary
	releases, _, err := a.repo.List(ctx, &strategy, nil, PhaseRunning, 100, 0)
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

// evaluateRelease 评估单个 running 的 canary 发布。
//   - canary 样本不足 → 本轮不动。
//   - 错误率/延迟超阈值 → 自动止损：权重归 0 + paused。
//   - 已达 TargetWeight → 不推进（等人工 promote）。
//   - 健康且未达目标 → 按 step 推进。
func (a *Auto) evaluateRelease(ctx context.Context, rel *Release) {
	cs, ok := a.metric.Report(ctx, rel.SecondaryVersionID.String())
	if !ok || cs.Requests < a.cfg.MinRequests {
		return
	}
	if a.shouldStopLoss(cs) {
		if _, err := a.svc.SetWeight(ctx, rel.ID, 0); err != nil {
			a.warn("auto canary stop-loss set weight failed", rel, err)
			return
		}
		if _, err := a.svc.Pause(ctx, rel.ID); err != nil {
			a.warn("auto canary stop-loss pause failed", rel, err)
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

	cfg := parseCanaryConfig(rel.Config)
	if rel.SecondaryWeight >= cfg.TargetWeight {
		return
	}
	next := rel.SecondaryWeight + cfg.StepWeight
	if next > cfg.TargetWeight {
		next = cfg.TargetWeight
	}
	if next <= rel.SecondaryWeight {
		return
	}
	if _, err := a.svc.SetWeight(ctx, rel.ID, next); err != nil {
		a.warn("auto canary step failed", rel, err)
		return
	}
	if a.logger != nil {
		a.logger.Info("auto canary stepped",
			zap.String("release", rel.ID.String()),
			zap.Int("from", rel.SecondaryWeight),
			zap.Int("to", next))
	}
}

func (a *Auto) warn(msg string, rel *Release, err error) {
	if a.logger != nil {
		a.logger.Warn(msg, zap.String("release", rel.ID.String()), zap.String("err", err.Error()))
	}
}

// shouldStopLoss 判定 canary 是否异常应止损。延迟阈值仅在配置 >0 时参与。
func (a *Auto) shouldStopLoss(cs VersionStat) bool {
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
