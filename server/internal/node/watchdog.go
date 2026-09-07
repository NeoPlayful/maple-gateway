package node

import (
	"context"
	"time"

	"go.uber.org/zap"
)

// WatchdogConfig 是节点心跳看护配置。
type WatchdogConfig struct {
	Interval time.Duration // 扫描周期
	Timeout  time.Duration // 心跳超时阈值（超过未心跳判 offline）
	Logger   *zap.Logger
}

// Watchdog 周期扫描节点心跳：超时未心跳的 online/maintenance 节点置 offline。
// 节点状态变化经 AutoRebuild 影响路由（其上实例被摘除）。ctx 取消退出。
func Watchdog(ctx context.Context, repo *Repository, cfg WatchdogConfig) {
	if cfg.Interval <= 0 {
		cfg.Interval = 10 * time.Second
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	go func() {
		t := time.NewTicker(cfg.Interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				cutoff := time.Now().Add(-cfg.Timeout)
				n, err := repo.MarkOffline(ctx, cutoff)
				if err != nil {
					if cfg.Logger != nil {
						cfg.Logger.Warn("node watchdog mark offline failed",
							zap.String("err", err.Error()))
					}
					continue
				}
				if n > 0 && cfg.Logger != nil {
					cfg.Logger.Info("node watchdog marked nodes offline",
						zap.Int64("count", n))
				}
			}
		}
	}()
}
