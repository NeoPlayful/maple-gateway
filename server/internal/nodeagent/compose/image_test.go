package compose

import (
	"os"
	"path/filepath"
	"testing"
)

const specWithImages = `services:
  web:
    image: nginx:alpine
    ports:
      - "18080:80"
  cache:
    image: redis:7-alpine
`

// 组成文件是镜像引用的权威来源：悬空镜像下 compose ps 会返回 sha256 摘要，
// 而这里读到的是创建时配置的名字。
func TestReadServiceImages(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "compose.yaml")
	if err := os.WriteFile(file, []byte(specWithImages), 0o644); err != nil {
		t.Fatal(err)
	}
	got := readServiceImages(file)
	if got["web"] != "nginx:alpine" || got["cache"] != "redis:7-alpine" {
		t.Fatalf("got %v", got)
	}
}

func TestReadServiceImagesMissingFile(t *testing.T) {
	if got := readServiceImages(filepath.Join(t.TempDir(), "nope.yaml")); got != nil {
		t.Errorf("expected nil for missing file, got %v", got)
	}
}

func TestReadServiceImagesBadYAML(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "compose.yaml")
	if err := os.WriteFile(file, []byte("services: [oops"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readServiceImages(file); got != nil {
		t.Errorf("expected nil for unparseable file, got %v", got)
	}
}

// 覆盖按服务名进行：摘要里退化的哈希被组成文件的名字替换。
func TestApplyConfigImagesOverridesDigest(t *testing.T) {
	svcs := []Service{
		{Service: "web", Image: "sha256:72ba65eb42c10344912a84ff42408db7d34f2feb642204570ab8fc5ffd29f1d3"},
		{Service: "cache", Image: "redis:7-alpine"},
	}
	applyConfigImages(svcs, map[string]string{"web": "nginx:alpine"})
	if svcs[0].Image != "nginx:alpine" {
		t.Errorf("web Image = %q", svcs[0].Image)
	}
	if svcs[1].Image != "redis:7-alpine" {
		t.Errorf("cache Image = %q, want untouched", svcs[1].Image)
	}
}

// 无配置镜像（如 specs 用 build 而非 image）时保留 ps 原值。
func TestApplyConfigImagesEmptyKeepsValue(t *testing.T) {
	svcs := []Service{{Service: "web", Image: "sha256:abc"}}
	applyConfigImages(svcs, nil)
	if svcs[0].Image != "sha256:abc" {
		t.Errorf("Image = %q, want preserved", svcs[0].Image)
	}
}
