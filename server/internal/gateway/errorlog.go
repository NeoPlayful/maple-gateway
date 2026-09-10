package gateway

import (
	"strings"

	"go.uber.org/zap"
)

// zapErrorLog 把 net/http.Server.ErrorLog（io.Writer，标准库 log 包格式）重定向到 zap。
//
// 目的：握手期错误不再走标准库那条格式不统一、且不受日志级别控制的裸日志行。
// 已知 SNI 哨兵错误（无证书 / 无 SNI）已由 certificate.Getter 的 onMiss 钩子在
// 未命中处带 sni + client_addr 记录，此处直接丢弃，避免一条错误两处重复输出。
// 其余错误按 Debug 记录，级别随全局日志级别走：默认 info 下静默，需要时开 debug。
type zapErrorLog struct {
	logger *zap.Logger
}

func newZapErrorLog(logger *zap.Logger) *zapErrorLog {
	return &zapErrorLog{logger: logger}
}

// Write 实现 io.Writer：缓冲被标准库 log 按行复用，故先剥时间戳前缀再判定。
func (w *zapErrorLog) Write(p []byte) (int, error) {
	n := len(p)
	msg := strings.TrimRight(string(p), "\r\n")
	if isSNISentinel(msg) {
		return n, nil
	}
	w.logger.Debug("http tls error", zap.String("err", trimLogPrefix(msg)))
	return n, nil
}

// isSNISentinel 判定是否为 SNI 未命中产生的哨兵错误（不重复记录）。
func isSNISentinel(msg string) bool {
	return strings.Contains(msg, "no certificate for server name") ||
		strings.Contains(msg, "client hello without server name")
}

// trimLogPrefix 去掉标准库 log 的时间戳前缀（"2026/09/10 10:23:03 http: ..." → "http: ..."）。
// 前缀固定 20 字节（"2006/01/02 15:04:05 "），以 s[4]、s[7] 是否为 '/' 识别。
func trimLogPrefix(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 20 && s[4] == '/' && s[7] == '/' {
		return strings.TrimSpace(s[20:])
	}
	return s
}
