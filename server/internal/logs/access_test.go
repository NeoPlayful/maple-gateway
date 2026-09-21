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
	all, totalAll := a.Query("", 0, "", time.Time{}, time.Time{}, 10, 0)
	if len(all) != 5 {
		t.Fatalf("got %d entries, want 5", len(all))
	}
	if totalAll != 5 {
		t.Fatalf("total=%d want 5", totalAll)
	}
	// 最新 = index4 → Timestamp = now+4s
	if all[0].Timestamp.Unix() != now.Add(4*time.Second).Unix() {
		t.Fatalf("latest first wrong: %v", all[0].Timestamp)
	}
	// 按状态过滤。
	ok, _ := a.Query("", 404, "", time.Time{}, time.Time{}, 10, 0)
	if len(ok) != 0 {
		t.Fatalf("filter 404 got %d", len(ok))
	}
	// 按 host 过滤 + offset：offset 影响返回条数，但不影响 total。
	sub, subTotal := a.Query("a.com", 0, "", time.Time{}, time.Time{}, 2, 1)
	if len(sub) != 2 {
		t.Fatalf("paged got %d", len(sub))
	}
	if subTotal != 5 {
		t.Fatalf("paged total=%d want 5 (offset 不应改变总数)", subTotal)
	}
	// 按 request_id 过滤。
	byRID, _ := a.Query("", 0, "rid-a", time.Time{}, time.Time{}, 10, 0)
	if len(byRID) != 5 {
		t.Fatalf("filter request_id got %d, want 5", len(byRID))
	}
	// 不存在的 request_id → 空。
	none, _ := a.Query("", 0, "rid-missing", time.Time{}, time.Time{}, 10, 0)
	if len(none) != 0 {
		t.Fatalf("filter missing request_id got %d, want 0", len(none))
	}
}

func TestAccessLog_QueryTotal(t *testing.T) {
	a := NewAccessLog(10)
	now := time.Unix(1_700_000_000, 0)
	for i := 0; i < 6; i++ {
		a.Append(AccessEntry{
			Timestamp: now.Add(time.Duration(i) * time.Second),
			Host:      "a.com", Status: 200,
		})
	}
	// limit 小于匹配总数：返回 limit 条，total 仍为匹配总数。
	items, total := a.Query("", 0, "", time.Time{}, time.Time{}, 2, 0)
	if len(items) != 2 {
		t.Fatalf("paged items=%d want 2", len(items))
	}
	if total != 6 {
		t.Fatalf("total=%d want 6", total)
	}
	// 按状态过滤后 total 只计匹配项。
	_, totalMiss := a.Query("", 500, "", time.Time{}, time.Time{}, 2, 0)
	if totalMiss != 0 {
		t.Fatalf("filtered total=%d want 0", totalMiss)
	}
}

func TestErrLog_QueryTotal(t *testing.T) {
	e := NewErrLog(10)
	now := time.Unix(1_700_000_000, 0)
	for i := 0; i < 4; i++ {
		e.Append(ErrEntry{Timestamp: now.Add(time.Duration(i) * time.Second), Host: "a.com", Status: 502, Error: "bad gateway"})
	}
	items, total := e.Query("", 0, "", time.Time{}, time.Time{}, 1, 0)
	if len(items) != 1 {
		t.Fatalf("items=%d want 1", len(items))
	}
	if total != 4 {
		t.Fatalf("total=%d want 4", total)
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
	all, _ := a.Query("", 0, "", time.Time{}, time.Time{}, 10, 0)
	if len(all) != 3 {
		t.Fatalf("queried %d, want 3", len(all))
	}
	// 保留的是最近 3 条（ts 2,3,4），最新 first。
	if all[0].Timestamp.Unix() != now.Add(4*time.Second).Unix() {
		t.Fatalf("ring did not keep newest: %v", all[0].Timestamp)
	}
}
