// Package ipam 提供项目级 IP 地址池的核心算法：CIDR 解析与校验、容量与序号换算、
// 重叠/含括判定、网关推导与保留段校验。
//
// 全部以标准 IPv4 数值演算，不做字符串拼接；序号与子网互转严格校验边界与溢出，
// 保证子网必然落在池内。
package ipam

import (
	"fmt"
	"net/netip"
)

// CIDR 是一个已规范化的 IPv4 网段（网络地址对齐）。
type CIDR struct {
	Prefix netip.Prefix
}

// Parse 解析并校验一个 IPv4 CIDR：合法前缀且地址为网络地址（主机位全零）。
// 主机位非零视为歧义输入直接拒绝，避免「看起来是子网、实际是某台主机」的误配。
func Parse(s string) (CIDR, error) {
	p, err := netip.ParsePrefix(s)
	if err != nil {
		return CIDR{}, fmt.Errorf("非法 CIDR %q: %w", s, err)
	}
	if !p.Addr().Is4() {
		return CIDR{}, fmt.Errorf("仅支持 IPv4，得到 %q", s)
	}
	if p.Addr() != p.Masked().Addr() {
		return CIDR{}, fmt.Errorf("CIDR %q 不是网络地址（主机位非零）", s)
	}
	return CIDR{Prefix: p}, nil
}

// MustParse 解析失败即 panic，仅供包内常量的初始化使用。
func MustParse(s string) CIDR {
	c, err := Parse(s)
	if err != nil {
		panic(err)
	}
	return c
}

// String 返回规范化的 CIDR 文本。
func (c CIDR) String() string { return c.Prefix.String() }

// Bits 返回前缀长度。
func (c CIDR) Bits() int { return c.Prefix.Bits() }

// Network 返回网络地址。
func (c CIDR) Network() netip.Addr { return c.Prefix.Masked().Addr() }

// lastAddr 返回网段末地址（广播地址，IPv4）。
func (c CIDR) lastAddr() netip.Addr {
	host := (uint32(1) << uint(32-c.Bits())) - 1
	return u32ToAddr(addrToU32(c.Network()) + host)
}

// validPrefix 校验「池前缀 → 项目前缀」的合法性：项目前缀须更长且不超过 /30。
func validPrefix(poolBits, subPrefix int) error {
	if subPrefix <= poolBits {
		return fmt.Errorf("项目前缀 /%d 必须大于池前缀 /%d", subPrefix, poolBits)
	}
	if subPrefix > 30 {
		return fmt.Errorf("项目前缀 /%d 超上限（须 <= 30）", subPrefix)
	}
	return nil
}

// Capacity 返回池在本项目前缀下可切分的子网数 = 2^(subPrefix - poolBits)。
func (c CIDR) Capacity(subPrefix int) (int64, error) {
	if err := validPrefix(c.Bits(), subPrefix); err != nil {
		return 0, err
	}
	n := subPrefix - c.Bits()
	// 位差 >=31 时容量在 int64 内仍可表示，但已远超任何实际节点规模，视为配置错误。
	if n >= 31 {
		return 0, fmt.Errorf("容量过大（位差 %d）", n)
	}
	return int64(1) << n, nil
}

// SubnetAt 由序号推子网：子网网络地址 = 池网络地址 + index << (32-subPrefix)。
// 序号越界、溢出或结果落在池外均报错。
func (c CIDR) SubnetAt(index int64, subPrefix int) (CIDR, error) {
	if index < 0 {
		return CIDR{}, fmt.Errorf("子网序号不可为负: %d", index)
	}
	cap, err := c.Capacity(subPrefix)
	if err != nil {
		return CIDR{}, err
	}
	if index >= cap {
		return CIDR{}, fmt.Errorf("子网序号 %d 越界（容量 %d）", index, cap)
	}
	// 用 uint64 中间量规避 index*step 的 uint32 溢出。
	step := uint64(1) << uint(32-subPrefix)
	base := uint64(addrToU32(c.Network()))
	v := base + uint64(index)*step
	if v > 0xFFFFFFFF {
		return CIDR{}, fmt.Errorf("子网序号 %d 溢出", index)
	}
	sub := CIDR{Prefix: netip.PrefixFrom(u32ToAddr(uint32(v)), subPrefix)}
	if !c.Contains(sub) {
		return CIDR{}, fmt.Errorf("子网 %s 落在池 %s 之外", sub, c)
	}
	return sub, nil
}

// Index 由子网反推序号（子网须完全落在池内）。
func (c CIDR) Index(sub CIDR) (int64, error) {
	if !c.Contains(sub) {
		return 0, fmt.Errorf("子网 %s 不在池 %s 内", sub, c)
	}
	step := uint64(1) << uint(32-sub.Bits())
	base := uint64(addrToU32(c.Network()))
	v := uint64(addrToU32(sub.Network()))
	return int64((v - base) / step), nil
}

// Contains 报告 sub 是否完全落在 c 内。
func (c CIDR) Contains(sub CIDR) bool {
	if sub.Bits() < c.Bits() {
		return false
	}
	return c.Prefix.Contains(sub.Network()) && c.Prefix.Contains(sub.lastAddr())
}

// Overlaps 报告两个网段是否存在地址重叠。
func (c CIDR) Overlaps(o CIDR) bool { return c.Prefix.Overlaps(o.Prefix) }

// Gateway 返回子网首可用地址（网络地址 + 1）。
func (c CIDR) Gateway() (netip.Addr, error) {
	if c.Bits() > 30 {
		return netip.Addr{}, fmt.Errorf("前缀 /%d 无可用主机地址", c.Bits())
	}
	return u32ToAddr(addrToU32(c.Network()) + 1), nil
}

// reservedRanges 是默认禁止作为项目池的地址段。
var reservedRanges = []CIDR{
	MustParse("0.0.0.0/8"),
	MustParse("127.0.0.0/8"),
	MustParse("169.254.0.0/16"),
	MustParse("224.0.0.0/4"),
	MustParse("240.0.0.0/4"),
}

// cgnatRange 是运营级 NAT 常用段，允许但警告。
var cgnatRange = MustParse("100.64.0.0/10")

// ValidatePool 校验池地址段是否可用：与保留段重叠即拒绝；与 CGNAT 段重叠返回警告。
func ValidatePool(p CIDR) (warning string, err error) {
	for _, r := range reservedRanges {
		if p.Overlaps(r) {
			return "", fmt.Errorf("池 %s 与保留段 %s 重叠", p, r)
		}
	}
	if p.Overlaps(cgnatRange) {
		return "该网段常用于 CGNAT 与 overlay/VPN，默认不推荐", nil
	}
	return "", nil
}

// ValidateSubnet 校验子网完全落在池内且前缀合法，返回规范化后的子网。
func ValidateSubnet(pool CIDR, subnet CIDR, subPrefix int) error {
	if err := validPrefix(pool.Bits(), subPrefix); err != nil {
		return err
	}
	if subnet.Bits() != subPrefix {
		return fmt.Errorf("子网 %s 前缀 /%d 与期望 /%d 不符", subnet, subnet.Bits(), subPrefix)
	}
	if !pool.Contains(subnet) {
		return fmt.Errorf("子网 %s 不在池 %s 内", subnet, pool)
	}
	return nil
}

func addrToU32(a netip.Addr) uint32 {
	b := a.As4()
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

func u32ToAddr(v uint32) netip.Addr {
	return netip.AddrFrom4([4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)})
}
