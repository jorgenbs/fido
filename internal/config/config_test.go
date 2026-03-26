// internal/config/config_test.go
package config

import (
	"os"
	"path/filepath"
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

	if cfg.Datadog.Token != "test-pat-token" {
		t.Errorf("expected token 'test-pat-token', got %q", cfg.Datadog.Token)
	}
	if len(cfg.Datadog.Services) != 2 {
		t.Errorf("expected 2 services, got %d", len(cfg.Datadog.Services))
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
	if cfg.Datadog.Site != "datadoghq.eu" {
		t.Errorf("expected default site 'datadoghq.eu', got %q", cfg.Datadog.Site)
	}
}
