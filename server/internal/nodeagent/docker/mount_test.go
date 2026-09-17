package docker

import "testing"

func TestWithinRoot(t *testing.T) {
	cases := []struct {
		root, child string
		want        bool
	}{
		{"/data/maple", "/data/maple", true},
		{"/data/maple", "/data/maple/t1/tpl/p1", true},
		{"/data/maple", "/data/maple-x/t1", false},
		{"/data/maple/", "/data/maple/t1", true},
		{"/data/maple", "/data/other", false},
		{"/data/maple", "/etc/passwd", false},
		{"", "/data/maple/t1", false},
	}
	for _, c := range cases {
		if got := withinRoot(c.root, c.child); got != c.want {
			t.Errorf("withinRoot(%q, %q) = %v, want %v", c.root, c.child, got, c.want)
		}
	}
}

func TestCleanSubdir(t *testing.T) {
	ok := []string{"t1", "t1/tpl/p1", "t1/tpl/p1/db-data"}
	for _, p := range ok {
		if _, err := cleanSubdir(p); err != nil {
			t.Errorf("cleanSubdir(%q) unexpected error: %v", p, err)
		}
	}
	bad := []string{"", "  ", "/abs", "../escape", "t1/../../etc", "t1//p2", "./t1", "C:/x"}
	for _, p := range bad {
		if _, err := cleanSubdir(p); err == nil {
			t.Errorf("cleanSubdir(%q) expected error", p)
		}
	}
}

func TestBuildBinds(t *testing.T) {
	c := &Client{dataRoot: "/data/maple"}

	t.Run("相对路径拼成数据根下绝对路径", func(t *testing.T) {
		got, err := c.buildBinds([]Mount{{
			Path: "t1/tpl/p1/db", Target: "/var/lib/postgresql/data",
		}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := "/data/maple/t1/tpl/p1/db:/var/lib/postgresql/data"
		if len(got) != 1 || got[0] != want {
			t.Fatalf("got %v, want [%s]", got, want)
		}
	})

	t.Run("只读挂载追加 ro", func(t *testing.T) {
		got, err := c.buildBinds([]Mount{{
			Path: "t1/conf", Target: "/etc/conf", ReadOnly: true,
		}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := "/data/maple/t1/conf:/etc/conf:ro"
		if len(got) != 1 || got[0] != want {
			t.Fatalf("got %v, want [%s]", got, want)
		}
	})

	t.Run("上跳穿越被拒绝", func(t *testing.T) {
		if _, err := c.buildBinds([]Mount{{
			Path: "../../etc", Target: "/etc",
		}}); err == nil {
			t.Fatal("expected error for traversal path")
		}
	})

	t.Run("绝对路径被拒绝", func(t *testing.T) {
		if _, err := c.buildBinds([]Mount{{
			Path: "/etc", Target: "/etc",
		}}); err == nil {
			t.Fatal("expected error for absolute path")
		}
	})

	t.Run("节点未配置数据根时拒绝带挂载创建", func(t *testing.T) {
		bare := &Client{}
		if _, err := bare.buildBinds([]Mount{{
			Path: "t1/tpl", Target: "/data",
		}}); err == nil {
			t.Fatal("expected error when data root is unset")
		}
	})

	t.Run("空 target 或相对 target 被拒绝", func(t *testing.T) {
		if _, err := c.buildBinds([]Mount{{Path: "t1", Target: ""}}); err == nil {
			t.Fatal("expected error for empty target")
		}
		if _, err := c.buildBinds([]Mount{{Path: "t1", Target: "relative"}}); err == nil {
			t.Fatal("expected error for relative target")
		}
	})

	t.Run("无挂载时返回空", func(t *testing.T) {
		got, err := c.buildBinds(nil)
		if err != nil || got != nil {
			t.Fatalf("got %v, err %v", got, err)
		}
	})
}
