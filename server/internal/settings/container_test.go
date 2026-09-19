package settings

import (
	"encoding/json"
	"testing"
)

// newTestRepo 构造一个仅内存缓存可用的 Repository（不触达 Ent，仅测缓存读取路径）。
func newTestRepo(entries ...Entry) *Repository {
	r := NewRepository(nil)
	for _, e := range entries {
		r.cache[string(e.Section)+":"+e.Key] = e
	}
	return r
}

// 无记录时 GetContainerNetwork 回退内建默认。
func TestGetContainerNetworkDefaults(t *testing.T) {
	r := newTestRepo()
	got := r.GetContainerNetwork()
	want := DefaultContainerNetwork()
	if got != want {
		t.Errorf("defaults = %+v, want %+v", got, want)
	}
}

// 有记录时按库值覆盖，非法值保留回退。
func TestGetContainerNetworkOverrides(t *testing.T) {
	pool := json.RawMessage(`"10.64.0.0/10"`)
	delay := json.RawMessage(`120`)
	reuse := json.RawMessage(`false`)
	badPrefix := json.RawMessage(`0`) // 非法（<=0）应保留默认 24
	r := newTestRepo(
		Entry{Section: SectionContainer, Key: KeyDefaultNetworkPool, Value: pool},
		Entry{Section: SectionContainer, Key: KeyDefaultReuseDelaySeconds, Value: delay},
		Entry{Section: SectionContainer, Key: KeyDefaultReuseEnabled, Value: reuse},
		Entry{Section: SectionContainer, Key: KeyDefaultProjectPrefix, Value: badPrefix},
	)
	got := r.GetContainerNetwork()
	if got.DefaultNetworkPool != "10.64.0.0/10" {
		t.Errorf("pool = %s, want 10.64.0.0/10", got.DefaultNetworkPool)
	}
	if got.DefaultReuseDelaySeconds != 120 {
		t.Errorf("delay = %d, want 120", got.DefaultReuseDelaySeconds)
	}
	if got.DefaultReuseEnabled {
		t.Error("reuse enabled should be false")
	}
	if got.DefaultProjectPrefix != DefaultProjectPrefix {
		t.Errorf("prefix = %d, want default %d", got.DefaultProjectPrefix, DefaultProjectPrefix)
	}
}

// DefaultContainerNetwork 与常量一致。
func TestDefaultContainerNetworkMatchesConstants(t *testing.T) {
	d := DefaultContainerNetwork()
	if d.DefaultNetworkPool != DefaultNetworkPool ||
		d.DefaultProjectPrefix != DefaultProjectPrefix ||
		d.AllocationMode != DefaultAllocationMode ||
		d.DefaultReuseEnabled != DefaultReuseEnabled ||
		d.DefaultReuseDelaySeconds != DefaultReuseDelaySeconds {
		t.Errorf("DefaultContainerNetwork mismatch: %+v", d)
	}
}
