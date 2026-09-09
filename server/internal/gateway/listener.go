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
				zap.String("scheme", scheme),
				zap.String("addr", ln.server.Addr),
				zap.String("tls_mode", string(ln.tlsMode)),
			)
			var err error
			if ln.tls {
				// 证书已预载进 srv.TLSConfig（Certificates 或 GetCertificate），
				// 传空 cert/key：ServeTLS 检测到 configHasCert=true 会跳过 LoadX509KeyPair，
				// 直接复用 TLSConfig（含 h2 NextProtos 自动配置）。
				err = ln.server.ListenAndServeTLS("", "")
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
