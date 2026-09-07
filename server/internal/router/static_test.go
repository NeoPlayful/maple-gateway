package router

import (
	"context"
	"errors"
	"testing"
)

func TestStaticResolver_Resolve(t *testing.T) {
	entries := map[string]StaticEntry{
		"shop-a.test":    {Scheme: "http", Address: "127.0.0.1:9101", Status: "active"},
		"disabled.test":  {Scheme: "http", Address: "127.0.0.1:9102", Status: "disabled"},
		"no-status.test": {Scheme: "", Address: "10.0.0.5:8080", Status: ""},
	}
	r := FromMap(entries)
	ctx := context.Background()

	t.Run("active route resolves", func(t *testing.T) {
		tg, err := r.Resolve(ctx, "shop-a.test")
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if tg.Scheme != "http" || tg.Host != "127.0.0.1:9101" {
			t.Fatalf("unexpected target: %+v", tg)
		}
	})

	t.Run("case and port normalized", func(t *testing.T) {
		tg, err := r.Resolve(ctx, "SHOP-A.TEST:8080")
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if tg.Host != "127.0.0.1:9101" {
			t.Fatalf("host should match after normalization, got %q", tg.Host)
		}
	})

	t.Run("default scheme is http", func(t *testing.T) {
		tg, err := r.Resolve(ctx, "no-status.test")
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if tg.Scheme != "http" {
			t.Fatalf("expected default http, got %q", tg.Scheme)
		}
	})

	t.Run("unknown host", func(t *testing.T) {
		_, err := r.Resolve(ctx, "unknown.test")
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("disabled host", func(t *testing.T) {
		_, err := r.Resolve(ctx, "disabled.test")
		if !errors.Is(err, ErrDomainDisabled) {
			t.Fatalf("expected ErrDomainDisabled, got %v", err)
		}
	})
}

func TestStaticResolver_YAML(t *testing.T) {
	raw := []byte(`
entries:
  a.test:
    address: "1.2.3.4:80"
  b.test:
    scheme: https
    address: "x.internal:443"
    status: disabled
`)
	r, err := NewStaticResolver(raw)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	tg, err := r.Resolve(context.Background(), "a.test")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if tg.Scheme != "http" || tg.Host != "1.2.3.4:80" {
		t.Fatalf("unexpected: %+v", tg)
	}
	if _, err := r.Resolve(context.Background(), "b.test"); !errors.Is(err, ErrDomainDisabled) {
		t.Fatalf("expected disabled err, got %v", err)
	}
}
