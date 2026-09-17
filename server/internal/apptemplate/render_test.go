package apptemplate

import "testing"

func TestRender(t *testing.T) {
	tmpl := &Template{
		Slug: "webapp",
		Spec: `services:
  web:
    image: {{image}}
    ports:
      - "{{port}}:80"
`,
		Params: []Param{
			{Key: "image", Type: ParamString, Required: true},
			{Key: "port", Type: ParamNumber, Default: "8080"},
		},
	}

	t.Run("用给定值渲染", func(t *testing.T) {
		out, err := Render(tmpl, map[string]string{"image": "nginx:alpine", "port": "9090"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if want := `image: nginx:alpine`; !contains(out, want) {
			t.Errorf("out missing %q:\n%s", want, out)
		}
		if want := `"9090:80"`; !contains(out, want) {
			t.Errorf("out missing %q:\n%s", want, out)
		}
	})

	t.Run("缺省值兜底", func(t *testing.T) {
		out, err := Render(tmpl, map[string]string{"image": "nginx"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !contains(out, `"8080:80"`) {
			t.Errorf("default not applied:\n%s", out)
		}
	})

	t.Run("必填缺值被拒绝", func(t *testing.T) {
		if _, err := Render(tmpl, map[string]string{}); err == nil {
			t.Fatal("expected error for missing required param")
		}
	})

	t.Run("number 非法被拒绝", func(t *testing.T) {
		if _, err := Render(tmpl, map[string]string{"image": "nginx", "port": "abc"}); err == nil {
			t.Fatal("expected error for non-numeric port")
		}
	})

	t.Run("规格引用未声明参数被拒绝", func(t *testing.T) {
		bad := &Template{Spec: "image: {{undeclared}}", Params: nil}
		if _, err := Render(bad, nil); err == nil {
			t.Fatal("expected error for undeclared placeholder")
		}
	})
}

func TestValidateParams(t *testing.T) {
	if err := ValidateParams([]Param{{Key: "image", Type: ParamString}}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	bad := []struct {
		name string
		ps   []Param
	}{
		{"键含横线", []Param{{Key: "a-b", Type: ParamString}}},
		{"键重复", []Param{{Key: "a", Type: ParamString}, {Key: "a", Type: ParamString}}},
		{"select 无选项", []Param{{Key: "a", Type: ParamSelect}}},
		{"空键", []Param{{Key: "", Type: ParamString}}},
		{"声明内置键", []Param{{Key: "data_path", Type: ParamString}}},
	}
	for _, c := range bad {
		if err := ValidateParams(c.ps); err == nil {
			t.Errorf("%s: expected error", c.name)
		}
	}
}

func TestRenderBuiltinDataPath(t *testing.T) {
	// 规格引用 data_path 但未声明：应由系统注入取值后渲染成功。
	tmpl := &Template{
		Slug:   "webapp",
		Spec:   "web:\n  image: {{image}}\n  vol: {{data_path}}/web\n",
		Params: []Param{{Key: "image", Type: ParamString, Required: true}},
	}
	out, err := Render(tmpl, map[string]string{
		"image": "nginx", "data_path": "t1/webapp/p1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !contains(out, "t1/webapp/p1/web") {
		t.Errorf("data_path not injected:\n%s", out)
	}

	// 未注入内置取值 → 拒绝，避免渲染出空段。
	if _, err := Render(tmpl, map[string]string{"image": "nginx"}); err == nil {
		t.Error("expected error when builtin data_path is not injected")
	}
}

func TestPlaceholders(t *testing.T) {
	got := Placeholders("a: {{x}}\nb: {{ y }}\nc: {{x}}\n")
	if len(got) != 2 || got[0] != "x" || got[1] != "y" {
		t.Fatalf("got %v", got)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
