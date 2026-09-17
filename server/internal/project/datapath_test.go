package project

import "testing"

func TestBuildDataPath(t *testing.T) {
	dp, err := BuildDataPath("tenant_1001", "webapp", "shop-a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dp.Rel != "tenant_1001/webapp/shop-a" {
		t.Fatalf("rel = %q", dp.Rel)
	}

	sub, err := dp.Sub("db")
	if err != nil {
		t.Fatalf("sub: %v", err)
	}
	if sub != "tenant_1001/webapp/shop-a/db" {
		t.Fatalf("sub = %q", sub)
	}

	// 空 sub 返回数据根相对路径本身。
	if got, _ := dp.Sub(""); got != dp.Rel {
		t.Fatalf("empty sub = %q", got)
	}

	bad := []struct {
		name                      string
		tenant, template, project string
	}{
		{"空项目名", "t1", "tpl", ""},
		{"项目名含上跳", "t1", "tpl", ".."},
		{"项目名含斜杠", "t1", "tpl", "a/b"},
		{"项目名含反斜杠", "t1", "tpl", `a\b`},
		{"项目名含空格", "t1", "tpl", "a b"},
		{"租户含中文", "租户", "tpl", "p"},
		{"模板含上跳", "t1", "..", "p"},
	}
	for _, c := range bad {
		if _, err := BuildDataPath(c.tenant, c.template, c.project); err == nil {
			t.Errorf("%s: expected error", c.name)
		}
	}
}

func TestSubRejectsTraversal(t *testing.T) {
	dp, _ := BuildDataPath("t1", "tpl", "p")
	for _, bad := range []string{"../etc", "/abs", "a/../b", "a//b"} {
		if _, err := dp.Sub(bad); err == nil {
			t.Errorf("Sub(%q): expected error", bad)
		}
	}
}
