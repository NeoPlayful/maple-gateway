package gateway

import (
	"errors"
	"net/http"

	"go.uber.org/zap"
)

// Start 在独立 goroutine 中监听并服务，返回错误通道。
// 服务正常关闭时错误通道收到 nil。
func (d *DataPlane) Start() <-chan error {
	errCh := make(chan error, 1)
	go func() {
		d.logger.Info("data plane listening", zap.String("addr", d.httpServer.Addr))
		if err := d.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()
	return errCh
}
