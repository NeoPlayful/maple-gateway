package compose

import (
	"strings"
	"testing"
)

func TestPrepareReplacesDataDirAndInjectsLabels(t *testing.T) {
	spec := `services:
  web:
    image: nginx:alpine
    ports:
      - "25000:80"
    volumes:
      - {{data_dir}}/t1/webapp/p1/web:/usr/share/nginx/html
`
	out, err := Prepare(spec, "/srv/maple", "maple.managed", "app-123")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if !strings.Contains(out, "/srv/maple/t1/webapp/p1/web") {
		t.Errorf("data_dir not replaced:\n%s", out)
	}
	if strings.Contains(out, DataDirPlaceholder) {
		t.Errorf("placeholder should be gone:\n%s", out)
	}
	if !strings.Contains(out, "maple.managed") {
		t.Errorf("managed label not injected:\n%s", out)
	}
	if !strings.Contains(out, "app-123") {
		t.Errorf("application_id not injected:\n%s", out)
	}
}

func TestPrepareDataDirWithoutRootRejected(t *testing.T) {
	spec := "services:\n  web:\n    image: nginx\n    volumes:\n      - {{data_dir}}/x:/y\n"
	if _, err := Prepare(spec, "", "maple.managed", "app-1"); err == nil {
		t.Fatal("expected error when data_dir referenced but data_root empty")
	}
}

func TestPrepareMergesExistingLabels(t *testing.T) {
	// 既有 labels 为 list 写法：合并后仍保留用户标签，并补上受管标签。
	spec := `services:
  web:
    image: nginx
    labels:
      - "app.role=frontend"
`
	out, err := Prepare(spec, "/srv", "maple.managed", "app-9")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if !strings.Contains(out, "app.role") {
		t.Errorf("existing label dropped:\n%s", out)
	}
	if !strings.Contains(out, "maple.managed") || !strings.Contains(out, "app-9") {
		t.Errorf("platform labels missing:\n%s", out)
	}
}

func TestPrepareNoServices(t *testing.T) {
	if _, err := Prepare("", "/srv", "maple.managed", "app-1"); err != nil {
		t.Fatalf("empty spec should be a no-op: %v", err)
	}
}
