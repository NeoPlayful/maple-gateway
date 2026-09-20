package settings

import (
	"encoding/json"
	"testing"
	"time"
)

// 无记录时 Health/Proxy 回退传入的 def。
func TestRuntimeFallbackToDefault(t *testing.T) {
	r := newTestRepo()
	defH := HealthRuntime{
		Interval:         20 * time.Second,
		Timeout:          5 * time.Second,
		FailureThreshold: 4,
		SuccessThreshold: 3,
		GracePeriod:      15 * time.Second,
	}
	gotH := r.Health(defH)
	if gotH != defH {
		t.Errorf("health = %+v, want def %+v", gotH, defH)
	}
	defP := DefaultProxyRuntime()
	if gotP := r.Proxy(defP); gotP != defP {
		t.Errorf("proxy = %+v, want def %+v", gotP, defP)
	}
}

// 库值覆盖 def；非法值保留 def。
func TestRuntimeOverridesAndRejectsInvalid(t *testing.T) {
	r := newTestRepo(
		Entry{Section: SectionHealth, Key: KeyHealthInterval, Value: json.RawMessage(`"30s"`)},
		Entry{Section: SectionHealth, Key: KeyHealthFailureThreshold, Value: json.RawMessage(`7`)},
		// 非法：阈值 0（<1）应保留 def。
		Entry{Section: SectionHealth, Key: KeyHealthSuccessThreshold, Value: json.RawMessage(`0`)},
		Entry{Section: SectionProxy, Key: KeyProxyMaxInFlight, Value: json.RawMessage(`8192`)},
		// 非法：时长不可解析应保留 def。
		Entry{Section: SectionProxy, Key: KeyProxyReadTimeout, Value: json.RawMessage(`"not-a-duration"`)},
	)
	defH := DefaultHealthRuntime()
	gotH := r.Health(defH)
	if gotH.Interval != 30*time.Second {
		t.Errorf("interval = %v, want 30s", gotH.Interval)
	}
	if gotH.FailureThreshold != 7 {
		t.Errorf("failure = %d, want 7", gotH.FailureThreshold)
	}
	if gotH.SuccessThreshold != defH.SuccessThreshold {
		t.Errorf("success = %d, want def %d", gotH.SuccessThreshold, defH.SuccessThreshold)
	}
	defP := DefaultProxyRuntime()
	gotP := r.Proxy(defP)
	if gotP.MaxInFlight != 8192 {
		t.Errorf("max_in_flight = %d, want 8192", gotP.MaxInFlight)
	}
	if gotP.ReadTimeout != defP.ReadTimeout {
		t.Errorf("read_timeout = %v, want def %v", gotP.ReadTimeout, defP.ReadTimeout)
	}
}

// 0 是 max_in_flight / max_body_bytes 的合法值（不限），不得被当非法回退。
func TestRuntimeZeroMeansUnlimited(t *testing.T) {
	r := newTestRepo(
		Entry{Section: SectionProxy, Key: KeyProxyMaxInFlight, Value: json.RawMessage(`0`)},
		Entry{Section: SectionProxy, Key: KeyProxyMaxBodyBytes, Value: json.RawMessage(`0`)},
	)
	got := r.Proxy(DefaultProxyRuntime())
	if got.MaxInFlight != 0 {
		t.Errorf("max_in_flight = %d, want 0", got.MaxInFlight)
	}
	if got.MaxBodyBytes != 0 {
		t.Errorf("max_body_bytes = %d, want 0", got.MaxBodyBytes)
	}
}

// 校验：合法值通过，非法值报错，未知键放行（交给其它分区逻辑）。
func TestValidateRuntimeKey(t *testing.T) {
	ok := func(section Section, key, raw string) {
		t.Helper()
		if err := ValidateRuntimeKey(section, key, json.RawMessage(raw)); err != nil {
			t.Errorf("expected ok for %s.%s=%s, got %v", section, key, raw, err)
		}
	}
	bad := func(section Section, key, raw string) {
		t.Helper()
		if err := ValidateRuntimeKey(section, key, json.RawMessage(raw)); err == nil {
			t.Errorf("expected error for %s.%s=%s", section, key, raw)
		}
	}
	ok(SectionHealth, KeyHealthInterval, `"10s"`)
	ok(SectionHealth, KeyHealthGracePeriod, `"0s"`)
	ok(SectionProxy, KeyProxyMaxInFlight, `0`)
	ok(SectionProxy, KeyProxyMaxBodyBytes, `0`)
	bad(SectionHealth, KeyHealthInterval, `"0s"`)
	bad(SectionHealth, KeyHealthFailureThreshold, `0`)
	bad(SectionProxy, KeyProxyReadTimeout, `"bad"`)
	bad(SectionProxy, KeyProxyMaxHeaderBytes, `0`)
	bad(SectionProxy, KeyProxyMaxInFlight, `-1`)
}
