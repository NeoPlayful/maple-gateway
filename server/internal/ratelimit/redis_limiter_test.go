package ratelimit

import (
	"testing"
	"time"
)

func TestFixedWindowKey_SameWindowSameKey(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	// 同一窗口内（60s 窗口）不同秒应同键。
	if k1 := fixedWindowKey("maple", "svc", 60, base); k1 != fixedWindowKey("maple", "svc", 60, base.Add(30*time.Second)) {
		t.Fatalf("same window should share key: %q vs %q", k1, fixedWindowKey("maple", "svc", 60, base.Add(30*time.Second)))
	}
	// 跨窗口应换键。
	if k1 := fixedWindowKey("maple", "svc", 60, base); k1 == fixedWindowKey("maple", "svc", 60, base.Add(61*time.Second)) {
		t.Fatalf("different windows should differ: %q", k1)
	}
}

func TestFixedWindowKey_IsolatedByKeyAndWindow(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	a1 := fixedWindowKey("maple", "svc-a", 60, base)
	b1 := fixedWindowKey("maple", "svc-b", 60, base)
	if a1 == b1 {
		t.Fatalf("different keys should differ: %q", a1)
	}
	// 窗口秒数不同 → 分桶粒度不同 → 键不同。
	small := fixedWindowKey("maple", "svc", 5, base)
	large := fixedWindowKey("maple", "svc", 60, base)
	if small == large {
		t.Fatalf("different window sec should differ")
	}
}

func TestFixedWindowKey_Prefix(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	got := fixedWindowKey("myapp", "svc", 60, base)
	if got != "myapp:rl:svc:"+itoa64(base.Unix()/60) {
		t.Fatalf("prefixed key = %q", got)
	}
	// 无前缀回退：直接 rl: 开头。
	if empty := fixedWindowKey("", "svc", 60, base); empty != "rl:svc:"+itoa64(base.Unix()/60) {
		t.Fatalf("empty prefix key = %q", empty)
	}
}

func TestNewRedisLimiter_NilRedis(t *testing.T) {
	if got := NewRedisLimiter(nil); got != nil {
		t.Fatal("nil redis should yield nil limiter")
	}
}
