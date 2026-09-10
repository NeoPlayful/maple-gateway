package acme

import (
	"testing"
	"time"
)

func TestChallengeStorePresentLookupClear(t *testing.T) {
	s := NewChallengeStore()
	s.Present("tok1", "tok1.thumb", time.Minute)
	if body, ok := s.Lookup("tok1"); !ok || body != "tok1.thumb" {
		t.Fatalf("lookup = %q,%v; want tok1.thumb,true", body, ok)
	}
	s.Clear("tok1")
	if _, ok := s.Lookup("tok1"); ok {
		t.Fatal("cleared token should not be found")
	}
}

func TestChallengeStoreExpiry(t *testing.T) {
	s := NewChallengeStore()
	s.Present("tok", "auth", time.Nanosecond)
	time.Sleep(2 * time.Millisecond)
	if _, ok := s.Lookup("tok"); ok {
		t.Fatal("expired token should not be found")
	}
	if s.Reap() != 1 {
		t.Fatal("Reap should remove the expired entry")
	}
	if s.Len() != 0 {
		t.Fatal("store should be empty after reap")
	}
}

func TestRespondPath(t *testing.T) {
	s := NewChallengeStore()
	s.Present("abc", "abc.thumb", time.Minute)

	// 命中挑战路径。
	if body, ok := s.RespondPath("/.well-known/acme-challenge/abc"); !ok || body != "abc.thumb" {
		t.Fatalf("challenge path = %q,%v; want abc.thumb,true", body, ok)
	}
	// 非挑战路径：不拦截，回落常规路由。
	if _, ok := s.RespondPath("/api/foo"); ok {
		t.Fatal("non-challenge path must not be intercepted")
	}
	// 未命中 token：不拦截。
	if _, ok := s.RespondPath("/.well-known/acme-challenge/unknown"); ok {
		t.Fatal("unknown token must not be intercepted")
	}
	// 带子路径：不拦截（防越权读取）。
	if _, ok := s.RespondPath("/.well-known/acme-challenge/abc/extra"); ok {
		t.Fatal("token with slash must not be intercepted")
	}
}
