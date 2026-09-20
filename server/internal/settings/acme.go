package settings

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/pkg"
)

// acme 运行时配置键名。存于 settings 表 acme 分区，缺行时回退调用方传入的
// 默认值（来自 config：内建默认 < YAML < env < DB）。
const (
	KeyACMEEnabled            = "enabled"
	KeyACMEDirectoryURL       = "directory_url"
	KeyACMEEmail              = "email"
	KeyACMEChallenge          = "challenge"
	KeyACMEKeyType            = "key_type"
	KeyACMERenewBefore        = "renew_before"
	KeyACMERenewCheckInterval = "renew_check_interval"
	KeyACMEMaxRenewAttempts   = "max_renew_attempts"
	KeyACMERateLimitBackoff   = "rate_limit_backoff"
)

// ACMERuntime 是 ACME 自动签发/续期的运行时参数。全部可热更：
// enabled 起停续期循环；续期参数下一轮生效；目录/邮箱/密钥类型下次签发生效。
type ACMERuntime struct {
	Enabled            bool          `json:"enabled"`
	DirectoryURL       string        `json:"directory_url"`
	Email              string        `json:"email"`
	Challenge          string        `json:"challenge"`
	KeyType            string        `json:"key_type"`
	RenewBefore        time.Duration `json:"-"`
	RenewCheckInterval time.Duration `json:"-"`
	MaxRenewAttempts   int           `json:"max_renew_attempts"`
	RateLimitBackoff   time.Duration `json:"-"`
}

// DefaultACMERuntime 返回 ACME 内建默认（与 config.Default 一致）。
func DefaultACMERuntime() ACMERuntime {
	return ACMERuntime{
		Enabled:            false,
		DirectoryURL:       "https://acme-v02.api.letsencrypt.org/directory",
		Challenge:          "http-01",
		KeyType:            "ec256",
		RenewBefore:        720 * time.Hour, // 30 天
		RenewCheckInterval: time.Hour,
		MaxRenewAttempts:   5,
		RateLimitBackoff:   6 * time.Hour,
	}
}

// normalize 用内建默认补齐非法字段，避免残缺 def 把 ACME 配成不可用。
func (a ACMERuntime) normalize() ACMERuntime {
	d := DefaultACMERuntime()
	if strings.TrimSpace(a.DirectoryURL) == "" {
		a.DirectoryURL = d.DirectoryURL
	}
	if a.Challenge != "http-01" && a.Challenge != "tls-alpn-01" {
		a.Challenge = d.Challenge
	}
	if a.KeyType != "ec256" && a.KeyType != "rsa2048" {
		a.KeyType = d.KeyType
	}
	if a.RenewBefore <= 0 {
		a.RenewBefore = d.RenewBefore
	}
	if a.RenewCheckInterval <= 0 {
		a.RenewCheckInterval = d.RenewCheckInterval
	}
	if a.MaxRenewAttempts < 1 {
		a.MaxRenewAttempts = d.MaxRenewAttempts
	}
	if a.RateLimitBackoff < 0 {
		a.RateLimitBackoff = d.RateLimitBackoff
	}
	return a
}

// ACME 读取 ACME 运行时配置（def 为 config 提供的默认，逐键被 DB 覆盖）。
func (r *Repository) ACME(def ACMERuntime) ACMERuntime {
	def = def.normalize()
	return ACMERuntime{
		Enabled:            r.GetBool(SectionACME, KeyACMEEnabled, def.Enabled),
		DirectoryURL:       r.GetString(SectionACME, KeyACMEDirectoryURL, def.DirectoryURL),
		Email:              r.GetString(SectionACME, KeyACMEEmail, def.Email),
		Challenge:          r.getStringEnum(SectionACME, KeyACMEChallenge, []string{"http-01", "tls-alpn-01"}, def.Challenge),
		KeyType:            r.getStringEnum(SectionACME, KeyACMEKeyType, []string{"ec256", "rsa2048"}, def.KeyType),
		RenewBefore:        r.getDuration(SectionACME, KeyACMERenewBefore, def.RenewBefore, time.Minute),
		RenewCheckInterval: r.getDuration(SectionACME, KeyACMERenewCheckInterval, def.RenewCheckInterval, time.Minute),
		MaxRenewAttempts:   r.getInt(SectionACME, KeyACMEMaxRenewAttempts, def.MaxRenewAttempts, 1),
		RateLimitBackoff:   r.getDuration(SectionACME, KeyACMERateLimitBackoff, def.RateLimitBackoff, 0),
	}
}

// getStringEnum 读一个枚举键：缺行/非法/不在 allowed 内时回退 def。
func (r *Repository) getStringEnum(section Section, key string, allowed []string, def string) string {
	e, ok := r.Get(section, key)
	if !ok {
		return def
	}
	var s string
	if json.Unmarshal(e.Value, &s) != nil {
		return def
	}
	for _, a := range allowed {
		if s == a {
			return s
		}
	}
	return def
}

// ValidateACMEKey 校验 acme 分区键的取值；未知键返回 nil。
func ValidateACMEKey(section Section, key string, raw json.RawMessage) error {
	if section != SectionACME {
		return nil
	}
	switch key {
	case KeyACMEEnabled:
		return validateBool(raw, key)
	case KeyACMEDirectoryURL:
		return validateURL(raw, key)
	case KeyACMEEmail:
		return validateEmail(raw, key)
	case KeyACMEChallenge:
		return validateEnum(raw, key, []string{"http-01"}, "暂仅支持 http-01")
	case KeyACMEKeyType:
		return validateEnum(raw, key, []string{"ec256", "rsa2048"}, "")
	case KeyACMERenewBefore:
		return validateDuration(raw, key, time.Minute)
	case KeyACMERenewCheckInterval:
		return validateDuration(raw, key, time.Minute)
	case KeyACMEMaxRenewAttempts:
		return validateInt(raw, key, 1)
	case KeyACMERateLimitBackoff:
		return validateDuration(raw, key, 0)
	}
	return nil
}

// ValidateACMEEnabled 校验跨字段约束：enabled=true 时 directory_url 与 email 必须齐备。
// 供 handler 在写入 enabled=true 时调用；缺字段给出可操作提示。
func ValidateACMEEnabled(enabled bool, directoryURL, email string) error {
	if !enabled {
		return nil
	}
	if strings.TrimSpace(directoryURL) == "" {
		return pkg.ErrValidation("启用 ACME 需先配置 directory_url")
	}
	if strings.TrimSpace(email) == "" {
		return pkg.ErrValidation("启用 ACME 需先配置 email（账户注册邮箱）")
	}
	return nil
}
