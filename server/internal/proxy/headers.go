package proxy

import (
	"net"
	"net/http"
	"strings"
)

// trustedProxyAddr 判断 r.RemoteAddr 是否来自受信代理。
// Phase 1 未接入可信列表时，仅对 loopback/私有网段按转发头补全（供本地开发）。
func trustedProxyAddr(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate()
}

// normalizeRemote 从 RemoteAddr 中取出纯 IP。
func clientIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}

// applyForwardedHeaders 补全 X-Forwarded-For / Proto / Host，并尽量保留原始。
//
// 规则（Phase 1 简化）：
//   - 若上游请求已带 X-Forwarded-For，则追加当前对端 IP（不覆盖已有值）。
//   - 若未带，则初始化为当前对端 IP。
//   - X-Forwarded-Proto 缺失时按请求 TLS 状态补全。
//
// S7 接入 Trusted Proxy 白名单后，将仅对受信来源做此类处理，避免伪造。
func applyForwardedHeaders(h http.Header, remoteAddr string, tlsOn bool) {
	xff := h.Values("X-Forwarded-For")
	peer := clientIP(remoteAddr)

	switch len(xff) {
	case 0:
		h.Set("X-Forwarded-For", peer)
	default:
		// 追加而非覆盖，保留代理链。
		h.Set("X-Forwarded-For", strings.Join(append(xff, peer), ", "))
	}

	if h.Get("X-Forwarded-Proto") == "" {
		if tlsOn {
			h.Set("X-Forwarded-Proto", "https")
		} else {
			h.Set("X-Forwarded-Proto", "http")
		}
	}
}
