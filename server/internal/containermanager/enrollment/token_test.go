package enrollment

import (
	"testing"
	"time"
)

func TestIssueRevokeConsume(t *testing.T) {
	s := NewTokenStore()

	tok, err := s.IssueBy("node-a", time.Hour, "admin-1")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if tok.CreatedBy != "admin-1" {
		t.Fatalf("created_by = %q", tok.CreatedBy)
	}
	if got := tok.DisplayStatus(time.Now()); got != TokenActive {
		t.Fatalf("status = %q, want active", got)
	}
	if err := s.Peek(tok.Value); err != nil {
		t.Fatalf("peek active: %v", err)
	}

	// 消费后：不可再 Peek，显示为 used，明文 value 被占位替换。
	if err := s.Consume(tok.Value, "11111111-1111-1111-1111-111111111111"); err != nil {
		t.Fatalf("consume: %v", err)
	}
	if err := s.Peek(tok.Value); err == nil {
		t.Fatal("consumed token should fail peek")
	}
	if got := tok.DisplayStatus(time.Now()); got != TokenUsed {
		t.Fatalf("status = %q, want used", got)
	}
	// 二次消费被拒。
	if err := s.Consume(tok.Value, "x"); err == nil {
		t.Fatal("double consume should fail")
	}
}

func TestRevokeOnlyActive(t *testing.T) {
	s := NewTokenStore()
	tok, _ := s.Issue("n", time.Hour)

	if !s.Revoke(tok.ID) {
		t.Fatal("revoke active should succeed")
	}
	if got := tok.DisplayStatus(time.Now()); got != TokenRevoked {
		t.Fatalf("status = %q, want revoked", got)
	}
	if err := s.Peek(tok.Value); err == nil {
		t.Fatal("revoked token should fail peek")
	}
	// 已撤销不可再撤销。
	if s.Revoke(tok.ID) {
		t.Fatal("second revoke should fail")
	}
}

func TestListSortedNewestFirst(t *testing.T) {
	s := NewTokenStore()
	a, _ := s.Issue("a", time.Hour)
	time.Sleep(2 * time.Millisecond)
	b, _ := s.Issue("b", time.Hour)

	list := s.List()
	if len(list) != 2 {
		t.Fatalf("len = %d", len(list))
	}
	if list[0].ID != b.ID || list[1].ID != a.ID {
		t.Fatalf("list not newest-first: %s, %s", list[0].ID, list[1].ID)
	}
}
