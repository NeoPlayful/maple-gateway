package router

import (
	"net"
	"strings"

	"golang.org/x/net/idna"
)

// 数据平面与存储统一使用的 hostname 规范化实现。
//
// 输入可能是：请求 Host 头（可含端口）、域名表单、用户填写的绑定域名。
// 输出统一为小写、无端口、IDN 已转 punycode 的规范 hostname。

var idnaProfile = idna.New(
	idna.MapForLookup(),
	idna.StrictDomainName(false),
	idna.Transitional(false),
)

// NormalizeHost 规范化域名：剥端口、转小写、IDN → punycode，并做结构校验。
// 畸形 host（含非法字符 / 空标签等）返回 ErrInvalidHost。
func NormalizeHost(host string) (string, error) {
	host = strings.TrimSpace(host)
	host = stripPort(host)
	if host == "" {
		return "", ErrInvalidHost
	}

	// IPv6 字面量（去掉括号后是纯 IPv6 地址）放行。
	if strings.Count(host, ":") > 0 {
		ip := net.ParseIP(host)
		if ip != nil && ip.To4() == nil {
			return host, nil
		}
		return "", ErrInvalidHost
	}

	// 剥离可能的尾部点（FQDN 形式）。
	host = strings.TrimSuffix(host, ".")
	if host == "" {
		return "", ErrInvalidHost
	}

	host = strings.ToLower(host)
	ascii, err := idnaProfile.ToASCII(host)
	if err != nil {
		return "", ErrInvalidHost
	}
	if !validLabeledName(ascii) {
		return "", ErrInvalidHost
	}
	return ascii, nil
}

// validLabeledName 校验规范 hostname：标签由字母数字/连字符组成，
// 不以连字符开头或结尾，标签非空。localhost 特例放行。
func validLabeledName(name string) bool {
	if name == "localhost" {
		return true
	}
	// 至少包含一个点，避免把单标签任意串当域名。
	if !strings.Contains(name, ".") {
		return false
	}
	for _, label := range strings.Split(name, ".") {
		if label == "" || len(label) > 63 {
			return false
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '-' {
				return false
			}
		}
	}
	return true
}

// stripPort 去掉 :port 后缀。IPv6 字面量（含 [] 或有多个冒号）不剥。
func stripPort(host string) string {
	if strings.Contains(host, "[") {
		// IPv6 字面量：形如 [::1]:8080
		if i := strings.Index(host, "]"); i >= 0 {
			return host[1:i] // 返回去掉方括号的地址
		}
		return host
	}
	if i := strings.LastIndex(host, ":"); i >= 0 {
		// 若冒号前仍有冒号，则是未加括号的 IPv6，不处理。
		if strings.Count(host, ":") == 1 {
			return host[:i]
		}
	}
	return host
}

// LookupHost 数据平面便捷包装：NormalizeHost 失败即 ErrInvalidHost。
func LookupHost(host string) (string, error) {
	return NormalizeHost(host)
}
