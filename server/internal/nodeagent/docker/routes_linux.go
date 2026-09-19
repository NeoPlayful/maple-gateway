//go:build linux

package docker

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"net/netip"
	"os"
	"strings"
)

// hostRoutes 读取 /proc/net/route，返回本机 IPv4 路由目标网段（CIDR 文本）。
//
// 该文件以十六进制小端存放目标地址；网关/掩码同为小端。仅取有掩码的条目，
// 跳过默认路由（目标 0.0.0.0）。用于检测网络池是否与宿主既有路由重叠。
func hostRoutes() []string {
	f, err := os.Open("/proc/net/route")
	if err != nil {
		return nil
	}
	defer f.Close()

	out := make([]string, 0, 8)
	seen := map[string]bool{}
	sc := bufio.NewScanner(f)
	sc.Scan() // 跳过表头
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 8 {
			continue
		}
		dst, err1 := hexLEToAddr(fields[1])
		mask, err2 := hexLEToAddr(fields[7])
		if err1 != nil || err2 != nil {
			continue
		}
		if dst.IsUnspecified() { // 默认路由跳过
			continue
		}
		bits, _ := mask.Prefix()
		if bits == 0 {
			continue
		}
		cidr := netip.PrefixFrom(dst, bits).Masked().String()
		if seen[cidr] {
			continue
		}
		seen[cidr] = true
		out = append(out, cidr)
	}
	return out
}

// hexLEToAddr 把 /proc/net/route 的十六进制小端 32 位值转为 IPv4 地址。
func hexLEToAddr(s string) (netip.Addr, error) {
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != 4 {
		return netip.Addr{}, fmt.Errorf("非法路由字段 %q", s)
	}
	// 文件按主机字节序（小端）存放，转为网络序。
	return netip.AddrFrom4([4]byte{b[3], b[2], b[1], b[0]}), nil
}
