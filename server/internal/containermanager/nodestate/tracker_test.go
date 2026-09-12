package nodestate

import (
	"testing"
	"time"
)

func TestStateProgression(t *testing.T) {
	// 极小阈值便于测试：10ms→unstable，20ms→offline。
	tr := New(10*time.Millisecond, 20*time.Millisecond)
	tr.Touch("n1")
	if got := tr.State("n1"); got != Online {
		t.Fatalf("want online, got %s", got)
	}

	time.Sleep(15 * time.Millisecond)
	ch := tr.Evaluate()
	if ch["n1"] != Unstable {
		t.Fatalf("want unstable, got %v", ch["n1"])
	}

	time.Sleep(15 * time.Millisecond)
	ch = tr.Evaluate()
	if ch["n1"] != Offline {
		t.Fatalf("want offline, got %v", ch["n1"])
	}
}

func TestTouchResetsToOnline(t *testing.T) {
	tr := New(10*time.Millisecond, 20*time.Millisecond)
	tr.Touch("n1")
	time.Sleep(15 * time.Millisecond)
	tr.Evaluate()  // → unstable
	tr.Touch("n1") // 心跳刷新
	if got := tr.State("n1"); got != Online {
		t.Fatalf("want online after touch, got %s", got)
	}
}

func TestDisconnectThenOffline(t *testing.T) {
	tr := New(10*time.Millisecond, 20*time.Millisecond)
	tr.Touch("n1")
	tr.Disconnect("n1")
	time.Sleep(25 * time.Millisecond)
	ch := tr.Evaluate()
	if ch["n1"] != Offline {
		t.Fatalf("want offline after disconnect, got %v", ch["n1"])
	}
}

func TestUnknownNodeIsOffline(t *testing.T) {
	tr := New(0, 0)
	if got := tr.State("ghost"); got != Offline {
		t.Fatalf("unknown node should be offline, got %s", got)
	}
}
