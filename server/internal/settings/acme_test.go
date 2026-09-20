package settings

import (
	"encoding/json"
	"testing"
	"time"
)

// 无记录时 ACME 回退传入的 def。
func TestACMEFallbackToDefault(t *testing.T) {
	r := newTestRepo()
	def := DefaultACMERuntime()
	if got := r.ACME(def); got != def {
		t.Errorf("acme = %+v, want def %+v", got, def)
	}
}

// 库值覆盖 def；非法值（枚举越界/时长不可解析/阈值越界）逐键回退 def。
func TestACMEOverridesAndRejectsInvalid(t *testing.T) {
	r := newTestRepo(
		Entry{Section: SectionACME, Key: KeyACMEEnabled, Value: json.RawMessage(`true`)},
		Entry{Section: SectionACME, Key: KeyACMEDirectoryURL, Value: json.RawMessage(`"https://acme-staging-v02.api.letsencrypt.org/directory"`)},
		Entry{Section: SectionACME, Key: KeyACMEKeyType, Value: json.RawMessage(`"rsa2048"`)},
		Entry{Section: SectionACME, Key: KeyACMERenewBefore, Value: json.RawMessage(`"360h"`)},
		// 非法：枚举越界应保留 def。
		Entry{Section: SectionACME, Key: KeyACMEChallenge, Value: json.RawMessage(`"dns-01"`)},
		// 非法：时长不可解析应保留 def。
		Entry{Section: SectionACME, Key: KeyACMERenewCheckInterval, Value: json.RawMessage(`"nope"`)},
		// 非法：阈值 <1 应保留 def。
		Entry{Section: SectionACME, Key: KeyACMEMaxRenewAttempts, Value: json.RawMessage(`0`)},
	)
	def := DefaultACMERuntime()
	got := r.ACME(def)
	if !got.Enabled {
		t.Error("enabled should be true")
	}
	if got.DirectoryURL != "https://acme-staging-v02.api.letsencrypt.org/directory" {
		t.Errorf("directory = %q", got.DirectoryURL)
	}
	if got.KeyType != "rsa2048" {
		t.Errorf("key_type = %q, want rsa2048", got.KeyType)
	}
	if got.RenewBefore != 360*time.Hour {
		t.Errorf("renew_before = %v, want 360h", got.RenewBefore)
	}
	if got.Challenge != def.Challenge {
		t.Errorf("challenge = %q, want def %q", got.Challenge, def.Challenge)
	}
	if got.RenewCheckInterval != def.RenewCheckInterval {
		t.Errorf("interval = %v, want def %v", got.RenewCheckInterval, def.RenewCheckInterval)
	}
	if got.MaxRenewAttempts != def.MaxRenewAttempts {
		t.Errorf("attempts = %d, want def %d", got.MaxRenewAttempts, def.MaxRenewAttempts)
	}
}

// 校验：acme 键合法值通过、非法值报错；未知键放行。
func TestValidateACMEKey(t *testing.T) {
	ok := func(key, raw string) {
		t.Helper()
		if err := ValidateRuntimeKey(SectionACME, key, json.RawMessage(raw)); err != nil {
			t.Errorf("expected ok for acme.%s=%s, got %v", key, raw, err)
		}
	}
	bad := func(key, raw string) {
		t.Helper()
		if err := ValidateRuntimeKey(SectionACME, key, json.RawMessage(raw)); err == nil {
			t.Errorf("expected error for acme.%s=%s", key, raw)
		}
	}
	ok(KeyACMEEnabled, `true`)
	ok(KeyACMEDirectoryURL, `"https://acme-v02.api.letsencrypt.org/directory"`)
	ok(KeyACMEEmail, `"admin@maple.com"`)
	ok(KeyACMEChallenge, `"http-01"`)
	ok(KeyACMEKeyType, `"ec256"`)
	ok(KeyACMERenewBefore, `"720h"`)
	ok(KeyACMERateLimitBackoff, `"0s"`)
	// tls-alpn-01 预留未实现：写入即拒绝（避免接受后运行期报错）。
	bad(KeyACMEChallenge, `"tls-alpn-01"`)
	bad(KeyACMEKeyType, `"ec384"`)
	bad(KeyACMEDirectoryURL, `"ftp://x"`)
	bad(KeyACMEEmail, `"not-an-email"`)
	bad(KeyACMERenewBefore, `"30s"`)
	bad(KeyACMEMaxRenewAttempts, `0`)
	bad(KeyACMEEnabled, `"yes"`)
	// 未知键放行（不归本函数管）。
	ok("some_unknown_key", `"whatever"`)
}

// enabled=true 的跨字段约束：目录与邮箱必须齐备。
func TestValidateACMEEnabled(t *testing.T) {
	if err := ValidateACMEEnabled(false, "", ""); err != nil {
		t.Errorf("disabled should not require fields, got %v", err)
	}
	if err := ValidateACMEEnabled(true, "https://ca/dir", "a@b.com"); err != nil {
		t.Errorf("complete config should pass, got %v", err)
	}
	if err := ValidateACMEEnabled(true, "", "a@b.com"); err == nil {
		t.Error("enabled without directory_url should fail")
	}
	if err := ValidateACMEEnabled(true, "https://ca/dir", ""); err == nil {
		t.Error("enabled without email should fail")
	}
}
