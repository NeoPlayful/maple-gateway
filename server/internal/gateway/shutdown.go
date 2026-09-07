package gateway

import (
	"context"
	"fmt"
)

// Shutdown 优雅关闭全部监听：先停止接收新连接，再等待在途请求结束。
func (d *DataPlane) Shutdown(ctx context.Context) error {
	d.logger.Info("data plane shutting down")
	for _, ln := range d.listeners {
		if err := ln.server.Shutdown(ctx); err != nil {
			return fmt.Errorf("data plane shutdown (%s): %w", ln.server.Addr, err)
		}
	}
	return nil
}
