package gateway

import (
	"errors"
	"net/http"

	"go.uber.org/zap"
)

// Start 为每个监听器启动独立 goroutine 服务，返回错误通道。
// 任一监听异常退出即上报；服务正常关闭时错误通道收到 nil。
func (d *DataPlane) Start() <-chan error {
	errCh := make(chan error, len(d.listeners))
	for _, ln := range d.listeners {
		ln := ln
		go func() {
			scheme := "http"
			if ln.tls {
				scheme = "https"
			}
			d.logger.Info("data plane listening",
				zap.String("scheme", scheme), zap.String("addr", ln.server.Addr))
			var err error
			if ln.tls {
				err = ln.server.ListenAndServeTLS(ln.cert, ln.key)
			} else {
				err = ln.server.ListenAndServe()
			}
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				errCh <- err
				return
			}
			errCh <- nil
		}()
	}
	return errCh
}
