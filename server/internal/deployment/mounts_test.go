package deployment

import "testing"

func TestValidateMounts(t *testing.T) {
	ok := []Mount{
		{Path: "tenant_1/webapp/shop-a/db", Target: "/var/lib/postgresql/data"},
		{Path: "tenant_1/webapp/shop-a/conf", Target: "/etc/conf", ReadOnly: true},
	}
	if err := ValidateMounts(ok); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := ValidateMounts(nil); err != nil {
		t.Fatalf("nil mounts should be valid: %v", err)
	}

	bad := []struct {
		name   string
		mounts []Mount
	}{
		{"绝对路径", []Mount{{Path: "/etc", Target: "/etc"}}},
		{"上跳穿越", []Mount{{Path: "a/../../etc", Target: "/etc"}}},
		{"点前缀", []Mount{{Path: "./a", Target: "/a"}}},
		{"空路径", []Mount{{Path: "", Target: "/a"}}},
		{"空 target", []Mount{{Path: "a", Target: ""}}},
		{"相对 target", []Mount{{Path: "a", Target: "relative"}}},
		{"盘符路径", []Mount{{Path: "C:/x", Target: "/x"}}},
		{"path 重复", []Mount{{Path: "a", Target: "/a"}, {Path: "a", Target: "/b"}}},
		{"target 重复", []Mount{{Path: "a", Target: "/x"}, {Path: "b", Target: "/x"}}},
		{"反斜杠穿越", []Mount{{Path: `a\..\..\etc`, Target: "/etc"}}},
	}
	for _, c := range bad {
		if err := ValidateMounts(c.mounts); err == nil {
			t.Errorf("%s: expected error", c.name)
		}
	}
}
