package docker

import (
	"testing"

	"github.com/docker/docker/api/types"
)

// 悬空镜像（tag 全部丢失）时，Docker 列表摘要的 Image 字段会退化成镜像 ID 哈希。
// 容器的展示镜像应取自身的 Config.Image，而非该摘要值。
func TestFromSummaryPrefersResolvedImage(t *testing.T) {
	s := types.Container{
		ID:      "abc123",
		Names:   []string{"/maple-31c6635dffc5-web-1"},
		Image:   "72ba65eb42c1", // 摘要里的退化值
		ImageID: "sha256:72ba65eb42c10344912a84ff42408db7d34f2feb642204570ab8fc5ffd29f1d3",
		State:   "running",
		Labels:  map[string]string{LabelInstanceID: "inst-1"},
	}
	got := fromSummary(s, "nginx:alpine")
	if got.Image != "nginx:alpine" {
		t.Errorf("Image = %q, want %q", got.Image, "nginx:alpine")
	}
	if got.InstanceID != "inst-1" {
		t.Errorf("InstanceID = %q", got.InstanceID)
	}
}

// 解析失败（image 为空）时回退摘要值，保证列表仍可用。
func TestFromSummaryFallsBackToSummaryImage(t *testing.T) {
	s := types.Container{ID: "abc123", Image: "redis:7-alpine", State: "running", Labels: map[string]string{}}
	if got := fromSummary(s, ""); got.Image != "redis:7-alpine" {
		t.Errorf("Image = %q, want summary fallback", got.Image)
	}
}

func TestImageRefKeyDistinguishesImage(t *testing.T) {
	if imageRefKey("c1", "i1") == imageRefKey("c1", "i2") {
		t.Error("different image IDs must yield different cache keys")
	}
	if imageRefKey("c1", "i1") == imageRefKey("c2", "i1") {
		t.Error("different container IDs must yield different cache keys")
	}
}
