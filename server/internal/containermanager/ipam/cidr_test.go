package ipam

import (
	"net/netip"
	"testing"
)

func TestParseRejectsHostBitsAndIPv6(t *testing.T) {
	if _, err := Parse("10.128.0.5/9"); err == nil {
		t.Error("expected error for non-network address")
	}
	if _, err := Parse("fd00::/8"); err == nil {
		t.Error("expected error for IPv6")
	}
	c, err := Parse("10.128.0.0/9")
	if err != nil {
		t.Fatalf("parse valid cidr: %v", err)
	}
	if c.String() != "10.128.0.0/9" {
		t.Errorf("normalized = %q", c.String())
	}
}

func TestCapacity(t *testing.T) {
	pool := MustParse("10.128.0.0/9")
	got, err := pool.Capacity(24)
	if err != nil {
		t.Fatalf("capacity: %v", err)
	}
	if got != 32768 {
		t.Errorf("capacity = %d, want 32768", got)
	}

	// 文档 §18：10.64.0.0/10 → /24 = 16384；172.20.0.0/14 → /24 = 1024。
	if n, _ := MustParse("10.64.0.0/10").Capacity(24); n != 16384 {
		t.Errorf("10.64/10 capacity = %d, want 16384", n)
	}
	if n, _ := MustParse("172.20.0.0/14").Capacity(24); n != 1024 {
		t.Errorf("172.20/14 capacity = %d, want 1024", n)
	}

	// 项目前缀必须大于池前缀。
	if _, err := pool.Capacity(8); err == nil {
		t.Error("expected error when subPrefix <= poolPrefix")
	}
	// 前缀上限 /30。
	if _, err := pool.Capacity(31); err == nil {
		t.Error("expected error when subPrefix > 30")
	}
}

// 文档 §77：index 0/255/256/32767 映射，32768 越界。
func TestSubnetAtAndIndex(t *testing.T) {
	pool := MustParse("10.128.0.0/9")
	cases := []struct {
		idx  int64
		want string
	}{
		{0, "10.128.0.0/24"},
		{255, "10.128.255.0/24"},
		{256, "10.129.0.0/24"},
		{32767, "10.255.255.0/24"},
	}
	for _, tc := range cases {
		sub, err := pool.SubnetAt(tc.idx, 24)
		if err != nil {
			t.Fatalf("SubnetAt(%d): %v", tc.idx, err)
		}
		if sub.String() != tc.want {
			t.Errorf("SubnetAt(%d) = %s, want %s", tc.idx, sub, tc.want)
		}
		// 反解序号须回到原值。
		back, err := pool.Index(sub)
		if err != nil {
			t.Fatalf("Index(%s): %v", sub, err)
		}
		if back != tc.idx {
			t.Errorf("Index(%s) = %d, want %d", sub, back, tc.idx)
		}
	}

	if _, err := pool.SubnetAt(32768, 24); err == nil {
		t.Error("expected out-of-range error for index 32768")
	}
	if _, err := pool.SubnetAt(-1, 24); err == nil {
		t.Error("expected error for negative index")
	}
}

func TestContainsAndOverlaps(t *testing.T) {
	pool := MustParse("10.128.0.0/9")
	in, _ := pool.SubnetAt(1, 24)
	if !pool.Contains(in) {
		t.Errorf("pool should contain %s", in)
	}
	outside := MustParse("10.0.0.0/24")
	if pool.Contains(outside) {
		t.Errorf("pool should not contain %s", outside)
	}
	// 同节点池间重叠检测（文档 §37）：/9 与 /10 重叠。
	if !pool.Overlaps(MustParse("10.128.0.0/10")) {
		t.Error("expected overlap between /9 and /10")
	}
	if pool.Overlaps(MustParse("10.64.0.0/10")) {
		t.Error("did not expect overlap between 10.128/9 and 10.64/10")
	}
}

func TestGateway(t *testing.T) {
	sub, _ := MustParse("10.128.0.0/9").SubnetAt(15, 24)
	gw, err := sub.Gateway()
	if err != nil {
		t.Fatalf("gateway: %v", err)
	}
	if gw.String() != "10.128.15.1" {
		t.Errorf("gateway = %s, want 10.128.15.1", gw)
	}
}

func TestValidatePoolReservedRanges(t *testing.T) {
	// 文档 §68：这些段必须拒绝。
	rejected := []string{"0.0.0.0/8", "127.0.0.0/8", "169.254.0.0/16", "224.0.0.0/4", "240.0.0.0/4"}
	for _, s := range rejected {
		if _, err := ValidatePool(MustParse(s)); err == nil {
			t.Errorf("expected %s to be rejected", s)
		}
	}

	// 默认池合法。
	if _, err := ValidatePool(MustParse("10.128.0.0/9")); err != nil {
		t.Errorf("default pool should be valid: %v", err)
	}

	// 文档 §69：100.64.0.0/10 允许但告警。
	warn, err := ValidatePool(MustParse("100.64.0.0/10"))
	if err != nil {
		t.Fatalf("CGNAT range should be allowed: %v", err)
	}
	if warn == "" {
		t.Error("expected a warning for CGNAT range")
	}
}

func TestValidateSubnet(t *testing.T) {
	pool := MustParse("10.128.0.0/9")
	sub, _ := pool.SubnetAt(3, 24)
	if err := ValidateSubnet(pool, sub, 24); err != nil {
		t.Errorf("valid subnet rejected: %v", err)
	}
	// 前缀不符。
	if err := ValidateSubnet(pool, sub, 25); err == nil {
		t.Error("expected prefix mismatch error")
	}
	// 池外子网。
	if err := ValidateSubnet(pool, MustParse("10.0.0.0/24"), 24); err == nil {
		t.Error("expected out-of-pool error")
	}
}

// 边界：池内最后一段子网须正确，且最后一子网的末地址等于池的广播地址。
func TestSubnetAtBoundaryReachesPoolEnd(t *testing.T) {
	pool := MustParse("10.128.0.0/9")
	last, err := pool.SubnetAt(32767, 24)
	if err != nil {
		t.Fatalf("last subnet: %v", err)
	}
	if last.Network() != netip.MustParseAddr("10.255.255.0") {
		t.Errorf("last subnet network = %s", last.Network())
	}
	if got, want := last.lastAddr(), pool.lastAddr(); got != want {
		t.Errorf("last subnet end = %s, pool end = %s", got, want)
	}
}
