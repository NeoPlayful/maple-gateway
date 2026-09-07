package cache

import (
	"context"
	"time"

	"go.uber.org/zap"
)

// AutoRebuild 周期性地从数据库全量重建路由表，实现无需重启的配置同步。
// 每次 Rebuild 失败仅记日志，保留最后有效表；ctx 取消时退出。
// 返回后由调用方持有（用于等待退出）。
func (c *Cache) AutoRebuild(ctx context.Context, interval time.Duration, logger *zap.Logger) {
	go func() {
		if interval <= 0 {
			interval = 5 * time.Second
		}
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := c.Rebuild(ctx); err != nil {
					logger.Warn("route cache auto-rebuild failed",
						zap.String("err", err.Error()))
				}
			}
		}
	}()
}
