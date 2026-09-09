package logs

import (
	"testing"
	"time"
)

func TestAccessLog_AppendQuery(t *testing.T) {
	a := NewAccessLog(10)
	now := time.Unix(1_700_000_000, 0)
	for i := 0; i < 5; i++ {
		a.Append(AccessEntry{
			Timestamp: now.Add(time.Duration(i) * time.Second),
			Host:      "a.com", Method: "GET", Path: "/", Status: 200, ClientIP: "1.2.3.4",
			RequestID: "rid-a",
		})
	}
	// 默认查询最新 5 条，倒序：最后 append 的在前。
	all := a.Query("", 0, "", time.Time{}, time.Time{}, 10, 0)
	if len(all) != 5 {
		t.Fatalf("got %d entries, want 5", len(all))
	}
	// 最新 = index4 → Timestamp = now+4s
	if all[0].Timestamp.Unix() != now.Add(4*time.Second).Unix() {
		t.Fatalf("latest first wrong: %v", all[0].Timestamp)
	}
	// 按状态过滤。
	ok := a.Query("", 404, "", time.Time{}, time.Time{}, 10, 0)
	if len(ok) != 0 {
		t.Fatalf("filter 404 got %d", len(ok))
	}
	// 按 host 过滤 + offset。
	sub := a.Query("a.com", 0, "", time.Time{}, time.Time{}, 2, 1)
	if len(sub) != 2 {
		t.Fatalf("paged got %d", len(sub))
	}
	// 按 request_id 过滤。
	byRID := a.Query("", 0, "rid-a", time.Time{}, time.Time{}, 10, 0)
	if len(byRID) != 5 {
		t.Fatalf("filter request_id got %d, want 5", len(byRID))
	}
	// 不存在的 request_id → 空。
	none := a.Query("", 0, "rid-missing", time.Time{}, time.Time{}, 10, 0)
	if len(none) != 0 {
		t.Fatalf("filter missing request_id got %d, want 0", len(none))
	}
}

func TestAccessLog_RingOverwrite(t *testing.T) {
	a := NewAccessLog(3)
	now := time.Unix(1_700_000_000, 0)
	for i := 0; i < 5; i++ {
		a.Append(AccessEntry{Timestamp: now.Add(time.Duration(i) * time.Second), Host: "a.com", Status: 200})
	}
	if a.Count() != 3 {
		t.Fatalf("count=%d want 3 (ring capped)", a.Count())
	}
	all := a.Query("", 0, "", time.Time{}, time.Time{}, 10, 0)
	if len(all) != 3 {
		t.Fatalf("queried %d, want 3", len(all))
	}
	// 保留的是最近 3 条（ts 2,3,4），最新 first。
	if all[0].Timestamp.Unix() != now.Add(4*time.Second).Unix() {
		t.Fatalf("ring did not keep newest: %v", all[0].Timestamp)
	}
}
