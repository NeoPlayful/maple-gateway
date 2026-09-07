package ratelimit

import (
	"testing"
	"time"
)

func TestLimiter_FixedWindowAllowsLimit(t *testing.T) {
	l := NewLimiter(0)
	now := time.Unix(1_700_000_000, 0)
	// 窗口 60s 配额 3：前 3 放行，第 4 拒绝。
	for i := 1; i <= 3; i++ {
		if a := l.Allow("svc", 3, 60, 0, now); !a.Allowed {
			t.Fatalf("request %d should be allowed", i)
		}
	}
	if a := l.Allow("svc", 3, 60, 0, now); a.Allowed {
		t.Fatal("4th request should be denied")
	}
}

func TestLimiter_WindowResetAfterPeriod(t *testing.T) {
	l := NewLimiter(0)
	now := time.Unix(1_700_000_000, 0)
	l.Allow("svc", 3, 60, 0, now)
	l.Allow("svc", 3, 60, 0, now)
	l.Allow("svc", 3, 60, 0, now)
	if a := l.Allow("svc", 3, 60, 0, now); a.Allowed {
		t.Fatal("should be full")
	}
	// 越过窗口 → 新窗口重新计数。
	next := now.Add(61 * time.Second)
	for i := 1; i <= 3; i++ {
		if a := l.Allow("svc", 3, 60, 0, next); !a.Allowed {
			t.Fatalf("window-reset request %d denied", i)
		}
	}
}

func TestLimiter_PerKeyIsolation(t *testing.T) {
	l := NewLimiter(0)
	now := time.Unix(1_700_000_000, 0)
	for i := 0; i < 10; i++ {
		l.Allow("svc", 2, 60, 0, now)
	}
	// 另一个 key 不受影响。
	if a := l.Allow("other", 2, 60, 0, now); !a.Allowed {
		t.Fatal("independent key should be allowed")
	}
}

func TestLimiter_BurstAllowsInstantOverLimit(t *testing.T) {
	l := NewLimiter(0)
	now := time.Unix(1_700_000_000, 0)
	// 配额 2/60s，burst 5：瞬时允许到 5。
	for i := 1; i <= 5; i++ {
		if a := l.Allow("svc", 2, 60, 5, now); !a.Allowed {
			t.Fatalf("burst request %d denied (want 5 bursts)", i)
		}
	}
	if a := l.Allow("svc", 2, 60, 5, now); a.Allowed {
		t.Fatal("6th should be denied beyond burst")
	}
}
