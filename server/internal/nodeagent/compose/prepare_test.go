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
	out, err := Prepare(spec, "/srv/maple", "maple.managed", "app-123", "")
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
	if _, err := Prepare(spec, "", "maple.managed", "app-1", ""); err == nil {
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
	out, err := Prepare(spec, "/srv", "maple.managed", "app-9", "")
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
	if _, err := Prepare("", "/srv", "maple.managed", "app-1", ""); err != nil {
		t.Fatalf("empty spec should be a no-op: %v", err)
	}
}

func TestPrepareInjectsExternalNetwork(t *testing.T) {
	spec := `services:
  web:
    image: nginx
  cache:
    image: redis
`
	out, err := Prepare(spec, "/srv", "maple.managed", "app-1", "maple-31c6635dffc5")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	// 顶层声明 external 网络并指向分配的网名。
	if !strings.Contains(out, "external: true") {
		t.Errorf("external network not declared:\n%s", out)
	}
	if !strings.Contains(out, "maple-31c6635dffc5") {
		t.Errorf("network name not injected:\n%s", out)
	}
	// 两个服务都应接入该项目网络。
	if strings.Count(out, "project_network") < 2 {
		t.Errorf("services not attached to project network:\n%s", out)
	}
}

func TestPrepareNetworkEmptyKeepsDefault(t *testing.T) {
	// 网络名为空时不注入 external 网络（沿用 Compose 默认网络）。
	spec := "services:\n  web:\n    image: nginx\n"
	out, err := Prepare(spec, "/srv", "maple.managed", "app-1", "")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if strings.Contains(out, "external") || strings.Contains(out, "project_network") {
		t.Errorf("no network should be injected when name empty:\n%s", out)
	}
}

func TestPrepareNetworkPreservesExistingMapNetworks(t *testing.T) {
	spec := `services:
  web:
    image: nginx
    networks:
      other:
        aliases:
          - web-alias
`
	out, err := Prepare(spec, "/srv", "maple.managed", "app-1", "maple-abc")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	// 既有网络及其别名须保留。
	if !strings.Contains(out, "web-alias") || !strings.Contains(out, "other") {
		t.Errorf("existing network config dropped:\n%s", out)
	}
	if !strings.Contains(out, "project_network") {
		t.Errorf("project network not added:\n%s", out)
	}
}
