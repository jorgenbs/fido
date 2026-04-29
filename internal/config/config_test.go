// internal/config/config_test.go
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yml")

	content := `
datadog:
  token: "test-pat-token"
  site: "datadoghq.eu"
  services:
    - "svc-a"
    - "svc-b"

scan:
  interval: "15m"
  since: "24h"

repositories:
  svc-a:
    local: "/path/to/svc-a"
  svc-b:
    git: "https://gitlab.com/org/svc-b.git"

agent:
  investigate: "claude -p"
  fix: "claude -p"
`
	os.WriteFile(configPath, []byte(content), 0644)

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Datadog[0].Token != "test-pat-token" {
		t.Errorf("expected token 'test-pat-token', got %q", cfg.Datadog[0].Token)
	}
	if len(cfg.Datadog[0].Services) != 2 {
		t.Errorf("expected 2 services, got %d", len(cfg.Datadog[0].Services))
	}
	if cfg.Repositories["svc-a"].Local != "/path/to/svc-a" {
		t.Errorf("expected local path, got %q", cfg.Repositories["svc-a"].Local)
	}
	if cfg.Repositories["svc-b"].Git != "https://gitlab.com/org/svc-b.git" {
		t.Errorf("expected git url, got %q", cfg.Repositories["svc-b"].Git)
	}
	if cfg.Agent.Investigate != "claude -p" {
		t.Errorf("expected agent investigate command, got %q", cfg.Agent.Investigate)
	}
}

func TestLoad_RateLimitDefaults(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yml")
	os.WriteFile(cfgPath, []byte("datadog:\n  token: test\n"), 0644)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Scan.RateLimit != 30 {
		t.Errorf("expected default rate limit 30, got %d", cfg.Scan.RateLimit)
	}
}

func TestLoad_RateLimitCustom(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yml")
	os.WriteFile(cfgPath, []byte("datadog:\n  token: test\nscan:\n  rate_limit: 60\n"), 0644)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Scan.RateLimit != 60 {
		t.Errorf("expected rate limit 60, got %d", cfg.Scan.RateLimit)
	}
}

func TestLoadConfig_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/config.yml")
	if err == nil {
		t.Fatal("expected error for missing config file")
	}
}

func TestLoadConfig_DefaultValues(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yml")

	content := `
datadog:
  token: "test-token"
`
	os.WriteFile(configPath, []byte(content), 0644)

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Scan.Interval != "15m" {
		t.Errorf("expected default interval '15m', got %q", cfg.Scan.Interval)
	}
	if cfg.Scan.Since != "24h" {
		t.Errorf("expected default since '24h', got %q", cfg.Scan.Since)
	}
	if cfg.Datadog[0].Site != "datadoghq.eu" {
		t.Errorf("expected default site 'datadoghq.eu', got %q", cfg.Datadog[0].Site)
	}
}

func TestDatadogConfigs_FlatFormat(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yml")

	content := `
datadog:
  token: "flat-token"
  site: "datadoghq.com"
  org_subdomain: "myorg"
  services:
    - "svc-x"
`
	os.WriteFile(cfgPath, []byte(content), 0644)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cfg.Datadog) != 1 {
		t.Fatalf("expected 1 datadog config, got %d", len(cfg.Datadog))
	}
	dd := cfg.Datadog[0]
	if dd.Token != "flat-token" {
		t.Errorf("expected token 'flat-token', got %q", dd.Token)
	}
	if dd.Site != "datadoghq.com" {
		t.Errorf("expected site 'datadoghq.com', got %q", dd.Site)
	}
	if dd.OrgSubdomain != "myorg" {
		t.Errorf("expected org_subdomain 'myorg', got %q", dd.OrgSubdomain)
	}
	if len(dd.Services) != 1 || dd.Services[0] != "svc-x" {
		t.Errorf("expected services [svc-x], got %v", dd.Services)
	}
	// Name is not set in flat format.
	if dd.Name != "" {
		t.Errorf("expected empty name in flat format, got %q", dd.Name)
	}
}

func TestDatadogConfigs_MultiFormat(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yml")

	content := `
datadog:
  prod:
    token: "prod-token"
    site: "datadoghq.eu"
    services:
      - "api"
  staging:
    token: "staging-token"
    services:
      - "api-staging"
`
	os.WriteFile(cfgPath, []byte(content), 0644)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cfg.Datadog) != 2 {
		t.Fatalf("expected 2 datadog configs, got %d", len(cfg.Datadog))
	}

	prod := cfg.Datadog[0]
	if prod.Name != "prod" {
		t.Errorf("expected name 'prod', got %q", prod.Name)
	}
	if prod.Token != "prod-token" {
		t.Errorf("expected token 'prod-token', got %q", prod.Token)
	}
	if prod.Site != "datadoghq.eu" {
		t.Errorf("expected site 'datadoghq.eu', got %q", prod.Site)
	}

	staging := cfg.Datadog[1]
	if staging.Name != "staging" {
		t.Errorf("expected name 'staging', got %q", staging.Name)
	}
	if staging.Token != "staging-token" {
		t.Errorf("expected token 'staging-token', got %q", staging.Token)
	}
	// Default site should be applied.
	if staging.Site != "datadoghq.eu" {
		t.Errorf("expected default site 'datadoghq.eu' for staging, got %q", staging.Site)
	}
	// Default org_subdomain should be applied.
	if staging.OrgSubdomain != "app" {
		t.Errorf("expected default org_subdomain 'app' for staging, got %q", staging.OrgSubdomain)
	}
}

func TestDatadogConfigs_AllServices(t *testing.T) {
	configs := DatadogConfigs{
		{Name: "work", Services: []string{"svc-a", "svc-b"}},
		{Name: "personal", Services: []string{"svc-c", "svc-d"}},
	}

	got := configs.AllServices()

	want := []string{"svc-a", "svc-b", "svc-c", "svc-d"}
	if len(got) != len(want) {
		t.Fatalf("expected %d services, got %d: %v", len(want), len(got), got)
	}
	for i, svc := range want {
		if got[i] != svc {
			t.Errorf("position %d: expected %q, got %q", i, svc, got[i])
		}
	}
}

func TestDatadogConfigs_ForService(t *testing.T) {
	configs := DatadogConfigs{
		{Name: "work", Services: []string{"svc-a", "svc-b"}},
		{Name: "personal", Services: []string{"svc-c", "svc-d"}},
	}

	// Service in second config.
	got := configs.ForService("svc-c")
	if got == nil {
		t.Fatal("expected non-nil config for 'svc-c'")
	}
	if got.Name != "personal" {
		t.Errorf("expected config name 'personal', got %q", got.Name)
	}

	// Service in first config.
	got = configs.ForService("svc-a")
	if got == nil {
		t.Fatal("expected non-nil config for 'svc-a'")
	}
	if got.Name != "work" {
		t.Errorf("expected config name 'work', got %q", got.Name)
	}

	// Unknown service returns nil.
	got = configs.ForService("does-not-exist")
	if got != nil {
		t.Errorf("expected nil for unknown service, got %+v", got)
	}
}

func TestDatadogConfigs_APIKeyFlatFormat(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yml")

	content := `
datadog:
  api_key: "my-api-key"
  app_key: "my-app-key"
  site: "datadoghq.com"
  services:
    - "svc-z"
`
	os.WriteFile(cfgPath, []byte(content), 0644)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cfg.Datadog) != 1 {
		t.Fatalf("expected 1 datadog config, got %d", len(cfg.Datadog))
	}
	dd := cfg.Datadog[0]
	if dd.APIKey != "my-api-key" {
		t.Errorf("expected api_key 'my-api-key', got %q", dd.APIKey)
	}
	if dd.AppKey != "my-app-key" {
		t.Errorf("expected app_key 'my-app-key', got %q", dd.AppKey)
	}
	if dd.Token != "" {
		t.Errorf("expected empty token, got %q", dd.Token)
	}
}

func TestDatadogConfig_HasAuth(t *testing.T) {
	tests := []struct {
		name string
		cfg  DatadogConfig
		want bool
	}{
		{"token", DatadogConfig{Token: "x"}, true},
		{"api+app key", DatadogConfig{APIKey: "x", AppKey: "y"}, true},
		{"no auth", DatadogConfig{}, false},
		{"api key only", DatadogConfig{APIKey: "x"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cfg.HasAuth()
			if got != tt.want {
				t.Errorf("HasAuth() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLoad_DuplicateServicesAcrossConfigs(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yml")

	content := `
datadog:
  work:
    token: "work-token"
    services:
      - "svc-overlap"
      - "svc-unique-work"
  personal:
    token: "personal-token"
    services:
      - "svc-overlap"
      - "svc-unique-personal"
`
	os.WriteFile(cfgPath, []byte(content), 0644)

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for duplicate service across configs, got nil")
	}
	if !strings.Contains(err.Error(), "svc-overlap") {
		t.Errorf("expected error to mention 'svc-overlap', got: %v", err)
	}
	if !strings.Contains(err.Error(), "work") || !strings.Contains(err.Error(), "personal") {
		t.Errorf("expected error to mention both config names, got: %v", err)
	}
}
