// Package security 提供网关安全基础能力。
package security

import (
	"fmt"
	"net"

	"github.com/NeoPlayful/maple-gateway/server/pkg"
)

// ValidateUpstreamAddress 校验 Instance 上游地址，防止 SSRF：
//   - 必须是 host 或 host:port；
//   - host 只能是内网/回环/私网 IP（或可解析到私网的域名，Phase 1 简化为 IP 校验）；
//   - 拒绝公网地址，避免租户将 Gateway 代理到任意外部地址。
func ValidateUpstreamAddress(address string) error {
	host := address
	if h, _, err := net.SplitHostPort(address); err == nil {
		host = h
	} else if _, _, err := net.SplitHostPort("[" + address + "]:0"); err == nil {
		host = address // 裸 IPv6
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return pkg.ErrValidation(fmt.Sprintf("上游地址 %q 必须是 IP 地址（主机名解析校验暂不支持）", address))
	}
	if !isInternalIP(ip) {
		return pkg.ErrValidation(fmt.Sprintf("上游地址 %q 不在允许的内网网段内", address))
	}
	return nil
}

// isInternalIP 判断是否为可作 upstream 的内网地址（回环/私网/链路本地）。
func isInternalIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}
