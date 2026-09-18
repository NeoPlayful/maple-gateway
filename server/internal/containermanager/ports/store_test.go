package ports

import (
	"context"
	"testing"
)

func TestAllocateUniqueAndRelease(t *testing.T) {
	ctx := context.Background()
	s := NewStore(nil, 20000, 20002)

	p1, err := s.Allocate(ctx, "proj-a", "project", "")
	if err != nil {
		t.Fatalf("allocate a: %v", err)
	}
	p2, err := s.Allocate(ctx, "proj-b", "project", "")
	if err != nil {
		t.Fatalf("allocate b: %v", err)
	}
	if p1 == p2 {
		t.Fatalf("expected distinct ports, got %d twice", p1)
	}

	// 同资源重复分配返回既有端口（幂等，不泄漏）。
	again, err := s.Allocate(ctx, "proj-a", "project", "")
	if err != nil {
		t.Fatalf("re-allocate a: %v", err)
	}
	if again != p1 {
		t.Fatalf("re-allocation changed port: got %d want %d", again, p1)
	}

	// 归还后可被重新分配。
	if err := s.Release(ctx, "proj-a"); err != nil {
		t.Fatalf("release a: %v", err)
	}
	if got := s.Allocated(); len(got) != 1 || got[0] != p2 {
		t.Fatalf("after release expected only %d, got %v", p2, got)
	}
}

func TestAllocateExhausted(t *testing.T) {
	ctx := context.Background()
	s := NewStore(nil, 20000, 20000) // 单端口区间
	if _, err := s.Allocate(ctx, "a", "project", ""); err != nil {
		t.Fatalf("first allocate: %v", err)
	}
	if _, err := s.Allocate(ctx, "b", "project", ""); err == nil {
		t.Fatal("expected exhaustion error")
	}
}
