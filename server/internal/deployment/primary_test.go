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

// decideSync 依剩余版本决定对齐 CM 期望态的动作：无版本→stop；有带镜像版本→push；
// 有版本但全无镜像→forget（清孤儿期望态、保留容器，而非 stop 误删）。
func TestDecideSync(t *testing.T) {
	withImage := &Version{Version: "v1", Image: "a:1", Status: VersionActive}
	noImage1 := &Version{Version: "v1", Image: "", Status: VersionStable}
	noImage2 := &Version{Version: "v2", Image: "", Status: VersionActive}

	if a, _ := decideSync(nil); a != syncStop {
		t.Errorf("no versions => syncStop, got %v", a)
	}
	if a, target := decideSync([]*Version{noImage1, withImage}); a != syncPush || target != withImage {
		t.Errorf("with image-bearing version => syncPush to it, got action=%v target=%v", a, target)
	}
	// 幸存版本全无镜像：必须 forget（保留容器），绝不能 stop（会整部署回收）。
	if a, target := decideSync([]*Version{noImage1, noImage2}); a != syncForget || target != nil {
		t.Errorf("all versions lack image => syncForget (not stop), got action=%v target=%v", a, target)
	}
}
