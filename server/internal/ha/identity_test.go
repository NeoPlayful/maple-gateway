package ha

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveIDConfiguredWins(t *testing.T) {
	id := ResolveID("gw-fixed", "", nil)
	if id != "gw-fixed" {
		t.Fatalf("configured id = %q, want gw-fixed", id)
	}
	// 配置优先于状态文件：即便状态文件存在也应忽略。
	dir := t.TempDir()
	path := filepath.Join(dir, "id")
	if err := os.WriteFile(path, []byte("from-file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := ResolveID("gw-fixed", path, nil); got != "gw-fixed" {
		t.Fatalf("configured id should win over state file, got %q", got)
	}
}

func TestResolveIDStateFileReuse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "id")

	first := ResolveID("", path, nil)
	if first == "" || len(first) != 12 {
		t.Fatalf("first resolved id = %q, want 12-char", first)
	}
	// 落盘后再次解析应复用同一 ID（模拟重启）。
	second := ResolveID("", path, nil)
	if second != first {
		t.Fatalf("state file reuse: first=%q second=%q", first, second)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != first {
		t.Fatalf("state file content = %q, want %q", string(raw), first)
	}
}

func TestResolveIDStateFileEmptyIsRewritten(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "id")
	if err := os.WriteFile(path, []byte("   \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := ResolveID("", path, nil)
	if got == "" || len(got) != 12 {
		t.Fatalf("empty state file should be rewritten with a fresh id, got %q", got)
	}
}

func TestShortIDFromSeedDeterministic(t *testing.T) {
	a := ShortIDFromSeed("gateway-node-1")
	b := ShortIDFromSeed("gateway-node-1")
	if a != b {
		t.Fatalf("same seed should be deterministic: %q vs %q", a, b)
	}
	if len(a) != 12 {
		t.Fatalf("derived id length = %d, want 12 (%q)", len(a), a)
	}
	if c := ShortIDFromSeed("gateway-node-2"); c == a {
		t.Fatalf("different seeds should differ: %q", c)
	}
}

func TestResolveIDHostnameFallbackDeterministic(t *testing.T) {
	// 无配置、无状态文件 → 主机名派生，同进程内应稳定。
	a := ResolveID("", "", nil)
	b := ResolveID("", "", nil)
	if a != b {
		t.Fatalf("hostname fallback should be stable: %q vs %q", a, b)
	}
}

func TestResolveIDStateFileUnwritableFallsBack(t *testing.T) {
	// 路径指向一个不存在的父目录下的 .tmp 不可写场景：用非法路径触发失败后应降级。
	// filepath 指向已存在的文件当目录，MkdirAll 会失败。
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	badPath := filepath.Join(blocker, "id") // blocker 是文件，无法作为目录
	got := ResolveID("", badPath, nil)
	if got == "" || len(got) != 12 {
		t.Fatalf("unwritable state file should fall back to hostname-derived id, got %q", got)
	}
}
