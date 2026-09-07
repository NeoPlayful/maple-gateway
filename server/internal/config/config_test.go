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

func TestValidate(t *testing.T) {
	cfg := Default()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default should validate ok: %v", err)
	}
	cfg.Gateway.HTTP.Enabled = false
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error when all listeners disabled")
	}
}
