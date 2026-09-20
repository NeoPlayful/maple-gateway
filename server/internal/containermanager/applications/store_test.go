package applications

import "testing"

// Put 应返回含新生成 id 的对象，且该 id 可立即被 Get 到（修复：创建接口返回空 id）。
func TestPutReturnsGeneratedID(t *testing.T) {
	s := NewStore(nil)
	out := s.Put(Application{Name: "a", Spec: "services: {}"})
	if out.ID == "" {
		t.Fatal("Put 返回空 id")
	}
	if _, ok := s.Get(out.ID); !ok {
		t.Fatalf("按返回的 id %q 取不到记录", out.ID)
	}
}

// Put 应收敛历史脏键：同一 id 存在键≠id 的残留条目时，写入后仅保留一条。
func TestPutConvergesStrayKey(t *testing.T) {
	s := NewStore(nil)
	s.apps["stray-key"] = Application{ID: "id-1", Name: "ghost"}
	s.apps["id-1"] = Application{ID: "id-1", Name: "real"}
	s.Put(Application{ID: "id-1", Name: "real2"})
	if got := len(s.List()); got != 1 {
		t.Fatalf("Put 后应仅剩 1 条，实际 %d 条", got)
	}
}

// Delete 应按 id 清掉键≠id 的残留条目（修复：删除后仍残留、列表继续重复）。
func TestDeleteByIDRemovesStrayKey(t *testing.T) {
	s := NewStore(nil)
	s.apps["stray-key"] = Application{ID: "id-2", Name: "ghost"}
	s.apps["id-2"] = Application{ID: "id-2", Name: "real"}
	s.Delete("id-2")
	if got := len(s.List()); got != 0 {
		t.Fatalf("Delete 后应剩 0 条，实际 %d 条", got)
	}
}

// ServiceForApplication：已绑定应用返回服务 ID，未绑定/不存在返回 false（观测器据此跳过）。
func TestServiceForApplication(t *testing.T) {
	s := NewStore(nil)
	s.Put(Application{ID: "app-bound", Name: "a", ServiceID: "svc-1"})
	s.Put(Application{ID: "app-unbound", Name: "b"})

	if got, ok := s.ServiceForApplication("app-bound"); !ok || got != "svc-1" {
		t.Errorf("bound = (%q,%v), want (svc-1,true)", got, ok)
	}
	if _, ok := s.ServiceForApplication("app-unbound"); ok {
		t.Error("unbound application must report no service")
	}
	if _, ok := s.ServiceForApplication("missing"); ok {
		t.Error("missing application must report no service")
	}
}
