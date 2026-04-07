// internal/config/config.go
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Datadog      DatadogConfig         `yaml:"datadog"`
	Scan         ScanConfig            `yaml:"scan"`
	Repositories map[string]RepoConfig `yaml:"repositories"`
	Agent        AgentConfig           `yaml:"agent"`
}

type DatadogConfig struct {
	Token        string   `yaml:"token"`
	Site         string   `yaml:"site"`
	OrgSubdomain string   `yaml:"org_subdomain"`
	Services     []string `yaml:"services"`
}

type ScanConfig struct {
	Interval  string `yaml:"interval"`
	Since     string `yaml:"since"`
	RateLimit int    `yaml:"rate_limit"`
}

type RepoConfig struct {
	Local string `yaml:"local"`
	Git   string `yaml:"git"`
}

type AgentConfig struct {
	Investigate string `yaml:"investigate"`
	Fix         string `yaml:"fix"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	cfg := &Config{
		Datadog: DatadogConfig{
			Site:         "datadoghq.eu",
			OrgSubdomain: "app",
		},
		Scan: ScanConfig{
			Interval:  "15m",
			Since:     "24h",
			RateLimit: 30,
		},
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	return cfg, nil
}
