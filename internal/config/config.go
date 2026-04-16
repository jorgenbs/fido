// internal/config/config.go
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Datadog      DatadogConfigs        `yaml:"datadog"`
	Scan         ScanConfig            `yaml:"scan"`
	Repositories map[string]RepoConfig `yaml:"repositories"`
	Agent        AgentConfig           `yaml:"agent"`
}

// DatadogConfigs is a slice of DatadogConfig that supports both flat (single-site)
// and named-map (multi-site) YAML formats.
type DatadogConfigs []DatadogConfig

type DatadogConfig struct {
	Name         string   `yaml:"-"`
	Token        string   `yaml:"token"`
	Site         string   `yaml:"site"`
	OrgSubdomain string   `yaml:"org_subdomain"`
	Services     []string `yaml:"services"`
}

// UnmarshalYAML handles two YAML formats:
//
//	Flat (single-site):
//	  datadog:
//	    token: "xxx"
//	    site: "datadoghq.eu"
//
//	Named-map (multi-site):
//	  datadog:
//	    prod:
//	      token: "xxx"
//	    staging:
//	      token: "yyy"
func (dc *DatadogConfigs) UnmarshalYAML(value *yaml.Node) error {
	// Detect flat format: the node contains a "token" mapping key directly.
	if value.Kind == yaml.MappingNode {
		for i := 0; i < len(value.Content)-1; i += 2 {
			if value.Content[i].Value == "token" {
				// Flat format — decode as a single DatadogConfig.
				var single DatadogConfig
				if err := value.Decode(&single); err != nil {
					return err
				}
				*dc = DatadogConfigs{single}
				return nil
			}
		}

		// Named-map format — decode as map[string]DatadogConfig.
		named := make(map[string]DatadogConfig)
		if err := value.Decode(&named); err != nil {
			return err
		}
		// Preserve insertion order using the YAML node's key order.
		result := make(DatadogConfigs, 0, len(named))
		for i := 0; i < len(value.Content)-1; i += 2 {
			key := value.Content[i].Value
			cfg, ok := named[key]
			if !ok {
				continue
			}
			cfg.Name = key
			result = append(result, cfg)
		}
		*dc = result
		return nil
	}

	return fmt.Errorf("datadog: expected a YAML mapping, got %v", value.Kind)
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
		Scan: ScanConfig{
			Interval:  "15m",
			Since:     "24h",
			RateLimit: 30,
		},
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	// Apply per-entry defaults for Datadog fields.
	for i := range cfg.Datadog {
		if cfg.Datadog[i].Site == "" {
			cfg.Datadog[i].Site = "datadoghq.eu"
		}
		if cfg.Datadog[i].OrgSubdomain == "" {
			cfg.Datadog[i].OrgSubdomain = "app"
		}
	}

	return cfg, nil
}
