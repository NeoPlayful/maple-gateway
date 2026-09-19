package deployment

import "testing"

// primaryVersion 决定版本集合变化后应下发哪个版本：
// 优先 stable 且带镜像者；无 stable 则退而取任一带镜像者；全无镜像返回 nil。
func TestPrimaryVersion(t *testing.T) {
	stable := &Version{Version: "v1", Image: "a:1", Status: VersionStable}
	canary := &Version{Version: "v2", Image: "b:1", Status: VersionCanary}
	noImage := &Version{Version: "v3", Image: "", Status: VersionStable}

	if got := primaryVersion([]*Version{canary, stable}); got != stable {
		t.Errorf("stable should win, got %v", got)
	}
	if got := primaryVersion([]*Version{canary}); got != canary {
		t.Errorf("fallback to any image-bearing version, got %v", got)
	}
	if got := primaryVersion([]*Version{noImage}); got != nil {
		t.Errorf("version without image should yield nil, got %v", got)
	}
	if got := primaryVersion(nil); got != nil {
		t.Errorf("empty set should yield nil, got %v", got)
	}
}
