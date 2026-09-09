package certificate

import (
	"testing"
)

func fakeLoaded(host string) *Loaded {
	return &Loaded{Hostname: host}
}

func TestCacheSetGet(t *testing.T) {
	c := NewCache()
	c.Set("Shop-A.Test", fakeLoaded("shop-a.test")) // 大小写/端口归一
	if got := c.Get("shop-a.test"); got == nil {
		t.Fatal("expected hit for shop-a.test")
	}
	if got := c.Get("SHOP-A.TEST:443"); got == nil {
		t.Fatal("expected hit with port/case variant")
	}
	if got := c.Get("shop-b.test"); got != nil {
		t.Fatal("unexpected hit for shop-b.test")
	}
	if got := c.Get(""); got != nil {
		t.Fatal("empty SNI must miss")
	}
}

func TestCacheDelete(t *testing.T) {
	c := NewCache()
	c.Set("shop-a.test", fakeLoaded("shop-a.test"))
	c.Delete("SHOP-A.TEST")
	if c.Len() != 0 {
		t.Fatal("delete should remove entry")
	}
}

func TestCacheReplaceAll(t *testing.T) {
	c := NewCache()
	c.Set("a.test", fakeLoaded("a.test"))
	c.ReplaceAll(map[string]*Loaded{
		"b.test": fakeLoaded("b.test"),
	})
	if c.Len() != 1 || c.Get("b.test") == nil || c.Get("a.test") != nil {
		t.Fatal("ReplaceAll must atomically swap content")
	}
}

func TestCacheLen(t *testing.T) {
	c := NewCache()
	c.Set("a.test", fakeLoaded("a.test"))
	c.Set("b.test", fakeLoaded("b.test"))
	if c.Len() != 2 {
		t.Fatalf("expected len 2, got %d", c.Len())
	}
}
