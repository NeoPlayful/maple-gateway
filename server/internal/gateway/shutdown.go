package gateway

import (
	"context"
	"fmt"
)

// Shutdown 优雅关闭：先停止接收新连接，再等待在途请求结束。
func (d *DataPlane) Shutdown(ctx context.Context) error {
	d.logger.Info("data plane shutting down")
	if err := d.httpServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("data plane shutdown: %w", err)
	}
	return nil
}
