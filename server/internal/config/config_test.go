package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoad_Defaults(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}
	if cfg.Gateway.HTTP.Address != ":8000" {
		t.Fatalf("default http addr = %q", cfg.Gateway.HTTP.Address)
	}
	if cfg.Management.Address != ":4000" {
		t.Fatalf("default mgmt addr = %q", cfg.Management.Address)
	}
	if cfg.Health.Interval != 10*time.Second {
		t.Fatalf("default health interval = %v", cfg.Health.Interval)
	}
	if cfg.HA.Heartbeat != 5*time.Second || cfg.HA.LeaseTTL != 15*time.Second {
		t.Fatalf("default ha = %+v", cfg.HA)
	}
	if cfg.HA.Enabled {
		t.Fatalf("ha should default disabled")
	}
	if cfg.CanaryAuto.Enabled {
		t.Fatalf("canary auto should default disabled")
	}
	if cfg.CanaryAuto.ErrRateMax != 5 {
		t.Fatalf("canary auto default err rate max = %v", cfg.CanaryAuto.ErrRateMax)
	}
}

func TestLoad_CanaryAutoEnv(t *testing.T) {
	t.Setenv("MAPLE_CANARY_AUTO_ENABLED", "true")
	t.Setenv("MAPLE_CANARY_AUTO_ERR_RATE_MAX", "8")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !cfg.CanaryAuto.Enabled {
		t.Fatalf("env canary auto enabled not applied")
	}
	if cfg.CanaryAuto.ErrRateMax != 8 {
		t.Fatalf("env canary auto err rate = %v", cfg.CanaryAuto.ErrRateMax)
	}
}

func TestLoad_HAEnvOverride(t *testing.T) {
	t.Setenv("MAPLE_HA_ENABLED", "true")
	t.Setenv("MAPLE_HA_INSTANCE_ID", "gw-1")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !cfg.HA.Enabled {
		t.Fatalf("env ha enabled not applied")
	}
	if cfg.HA.InstanceID != "gw-1" {
		t.Fatalf("env ha instance id = %q", cfg.HA.InstanceID)
	}
}

func TestLoad_YAMLOverride(t *testing.T) {
	path := writeTemp(t, `
gateway:
  http:
    address: ":9090"
management:
  address: ":5000"
health:
  failure_threshold: 5
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Gateway.HTTP.Address != ":9090" {
		t.Fatalf("yaml http addr = %q", cfg.Gateway.HTTP.Address)
	}
	if cfg.Management.Address != ":5000" {
		t.Fatalf("yaml mgmt addr = %q", cfg.Management.Address)
	}
	if cfg.Health.FailureThreshold != 5 {
		t.Fatalf("yaml threshold = %d", cfg.Health.FailureThreshold)
	}
	// 未覆盖的字段应保留默认。
	if cfg.Health.Interval != 10*time.Second {
		t.Fatalf("default interval lost = %v", cfg.Health.Interval)
	}
}

func TestLoad_EnvOverride(t *testing.T) {
	t.Setenv("MAPLE_GATEWAY_HTTP_ADDR", ":7777")
	t.Setenv("MAPLE_MANAGEMENT_ADDR", ":6666")
	t.Setenv("MAPLE_DATABASE_URL", "postgres://env:env@db/maple")
	t.Setenv("MAPLE_ADMIN_TOKEN", "env-token")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Gateway.HTTP.Address != ":7777" {
		t.Fatalf("env http addr = %q", cfg.Gateway.HTTP.Address)
	}
	if cfg.Management.Address != ":6666" {
		t.Fatalf("env mgmt addr = %q", cfg.Management.Address)
	}
	if cfg.Database.URL != "postgres://env:env@db/maple" {
		t.Fatalf("env db url = %q", cfg.Database.URL)
	}
	if cfg.Security.AdminToken != "env-token" {
		t.Fatalf("env admin token = %q", cfg.Security.AdminToken)
	}
}

func TestLoad_EnvBeatsYAML(t *testing.T) {
	path := writeTemp(t, `
gateway:
  http:
    address: ":9090"
`)
	t.Setenv("MAPLE_GATEWAY_HTTP_ADDR", ":7777")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Gateway.HTTP.Address != ":7777" {
		t.Fatalf("env should beat yaml, got %q", cfg.Gateway.HTTP.Address)
	}
}

func TestLoad_CertEncKeyFromYAMLAndEnv(t *testing.T) {
	// yaml 提供 tls.cert_enc_key。
	path := writeTemp(t, `
tls:
  cert_enc_key: "yaml-key"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.TLS.CertEncKey != "yaml-key" {
		t.Fatalf("yaml cert_enc_key = %q", cfg.TLS.CertEncKey)
	}

	// 环境变量 MAPLE_TLS_CERT_ENC_KEY 优先于 yaml。
	t.Setenv("MAPLE_TLS_CERT_ENC_KEY", "env-key")
	cfg2, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg2.TLS.CertEncKey != "env-key" {
		t.Fatalf("env cert_enc_key should beat yaml, got %q", cfg2.TLS.CertEncKey)
	}
}

func TestValidate(t *testing.T) {
	cfg := Default()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default should validate ok: %v", err)
	}
	cfg.Gateway.HTTP.Enabled = false
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error when all listeners disabled")
	}
	// ha.enabled 需要 database.url。
	cfg2 := Default()
	cfg2.HA.Enabled = true
	cfg2.Database.URL = ""
	if err := cfg2.Validate(); err == nil {
		t.Fatal("expected error when ha enabled without database.url")
	}
	cfg2.Database.URL = "postgres://u:p@db/maple"
	if err := cfg2.Validate(); err != nil {
		t.Fatalf("ha enabled with db url should validate ok: %v", err)
	}
}
