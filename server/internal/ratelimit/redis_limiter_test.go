package ratelimit

import (
	"testing"
	"time"
)

func TestFixedWindowKey_SameWindowSameKey(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	// 同一窗口内（60s 窗口）不同秒应同键。
	if k1 := fixedWindowKey("svc", 60, base); k1 != fixedWindowKey("svc", 60, base.Add(30*time.Second)) {
		t.Fatalf("same window should share key: %q vs %q", k1, fixedWindowKey("svc", 60, base.Add(30*time.Second)))
	}
	// 跨窗口应换键。
	if k1 := fixedWindowKey("svc", 60, base); k1 == fixedWindowKey("svc", 60, base.Add(61*time.Second)) {
		t.Fatalf("different windows should differ: %q", k1)
	}
}

func TestFixedWindowKey_IsolatedByKeyAndWindow(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	a1 := fixedWindowKey("svc-a", 60, base)
	b1 := fixedWindowKey("svc-b", 60, base)
	if a1 == b1 {
		t.Fatalf("different keys should differ: %q", a1)
	}
	// 窗口秒数不同 → 分桶粒度不同 → 键不同。
	small := fixedWindowKey("svc", 5, base)
	large := fixedWindowKey("svc", 60, base)
	if small == large {
		t.Fatalf("different window sec should differ")
	}
}

func TestNewRedisLimiter_NilRedis(t *testing.T) {
	if got := NewRedisLimiter(nil); got != nil {
		t.Fatal("nil redis should yield nil limiter")
	}
}
