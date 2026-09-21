package proxy

import (
	"fmt"
	"net"
	"net/http"
	"strings"
)

// ProxyTrust 判定请求对端是否为可信代理，仅对可信来源采信转发头。
//
// 空集合（含 nil 接收者）表示「无可信代理」：所有入站转发头一律忽略，
// 客户端 IP 回退到不可伪造的 socket 对端。这是安全的默认——未显式配置
// 白名单时宁可少认，也不让日志被伪造头污染。
type ProxyTrust struct {
	nets []*net.IPNet
	ips  []net.IP
}

// NewProxyTrust 解析可信代理来源列表，支持 CIDR（10.0.0.0/8）与单 IP（203.0.113.5）。
// 解析失败返回错误，调用方应视为「无可信代理」（fail closed）而非放行。
func NewProxyTrust(specs []string) (*ProxyTrust, error) {
	t := &ProxyTrust{}
	for _, raw := range specs {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		if strings.Contains(s, "/") {
			_, n, err := net.ParseCIDR(s)
			if err != nil {
				return nil, fmt.Errorf("trusted proxy %q: %w", s, err)
			}
			t.nets = append(t.nets, n)
			continue
		}
		ip := net.ParseIP(s)
		if ip == nil {
			return nil, fmt.Errorf("trusted proxy %q: not an IP or CIDR", s)
		}
		t.ips = append(t.ips, ip)
	}
	return t, nil
}

// Trusted 判断 host（纯 IP 字符串）是否落在可信代理集合内。
func (t *ProxyTrust) Trusted(host string) bool {
	if t == nil {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, n := range t.nets {
		if n.Contains(ip) {
			return true
		}
	}
	for _, known := range t.ips {
		if known.Equal(ip) {
			return true
		}
	}
	return false
}

// trustedPeer 判断 RemoteAddr 是否来自可信代理。
func (t *ProxyTrust) trustedPeer(remoteAddr string) bool {
	return t.Trusted(clientIP(remoteAddr))
}

// clientIP 从 RemoteAddr 中取出纯 IP。
func clientIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}

// requestScheme 判定入站请求协议（TLS 连接为 https，否则 http）。
func requestScheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

// maxUAOrHeaderLen 限制写入日志的 UA / 转发头长度：访问日志是常驻内存的环形缓冲，
// 过长字段会显著抬高单条体积，截断后加省略号。
const maxUAOrHeaderLen = 256

// truncateField 截断超长字段（UA / 转发头），保留前缀并标注已截断。
func truncateField(v string) string {
	if len(v) <= maxUAOrHeaderLen {
		return v
	}
	return v[:maxUAOrHeaderLen] + "..."
}

// truncateUA 截断超长 User-Agent。
func truncateUA(ua string) string { return truncateField(ua) }

// rawXFF 汇总全部 X-Forwarded-For 头行为单串（多行以逗号连接），供日志留证。
func rawXFF(r *http.Request) string {
	return strings.Join(r.Header.Values("X-Forwarded-For"), ", ")
}

// realClientIP 在「对端可信」前提下推导真实客户端 IP。
//
// 算法：把 X-Forwarded-For 展开为转发链后从最右端向左扫描，跳过其中落在可信
// 集合内的跳，返回第一个非可信 IP——它是由最近一个可信代理写入、客户端无法逾越的
// 那一跳。整条链皆可信（或缺失 XFF）时回退到单值的 X-Real-IP。
//
// 对端不可信、链中出现畸形条目、或无法判定时一律返回空串：调用方据此不采信任何
// 客户端可伪造的值，宁缺毋滥。
func (t *ProxyTrust) realClientIP(r *http.Request) string {
	if !t.trustedPeer(r.RemoteAddr) {
		return ""
	}

	var chain []string
	for _, v := range r.Header.Values("X-Forwarded-For") {
		for _, part := range strings.Split(v, ",") {
			if p := strings.TrimSpace(part); p != "" {
				chain = append(chain, p)
			}
		}
	}
	for i := len(chain) - 1; i >= 0; i-- {
		hop := chain[i]
		if net.ParseIP(hop) == nil {
			return "" // 畸形条目：链不可信，放弃推导
		}
		if t.Trusted(hop) {
			continue // 受信跳，继续向左寻找真实来源
		}
		return hop
	}

	// 无 XFF 或整链皆受信：用 X-Real-IP 兜底（仅当其自身非受信代理）。
	if v := strings.TrimSpace(r.Header.Get("X-Real-IP")); v != "" {
		if ip := net.ParseIP(v); ip != nil && !t.Trusted(v) {
			return v
		}
	}
	return ""
}

// applyForwardedHeaders 补全 X-Forwarded-For / Proto，并尽量保留原始。
//
// 规则：
//   - 对端可信：保留入站 XFF 链并在末尾追加当前对端 IP（不覆盖已有值）。
//   - 对端不可信：客户端可伪造入站 XFF，一律丢弃，仅写入当前对端 IP，
//     避免把伪造链路透传给上游。
//   - X-Forwarded-Proto 缺失时按请求 TLS 状态补全（保留既有值）。
func (t *ProxyTrust) applyForwardedHeaders(h http.Header, remoteAddr string, tlsOn bool) {
	peer := clientIP(remoteAddr)

	var prior []string
	if t.trustedPeer(remoteAddr) {
		prior = h.Values("X-Forwarded-For")
	}
	h.Del("X-Forwarded-For")
	if len(prior) == 0 {
		h.Set("X-Forwarded-For", peer)
	} else {
		h.Set("X-Forwarded-For", strings.Join(append(prior, peer), ", "))
	}

	if h.Get("X-Forwarded-Proto") == "" {
		if tlsOn {
			h.Set("X-Forwarded-Proto", "https")
		} else {
			h.Set("X-Forwarded-Proto", "http")
		}
	}
}
