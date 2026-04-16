# Multi-Datadog Config Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Support multiple Datadog sites/orgs in the config while keeping backward compatibility with the existing single-config format.

**Architecture:** Custom YAML unmarshaling detects whether `datadog:` is a flat config (has `token` key) or a named map of configs. The `Config.Datadog` field becomes a `DatadogConfigs` slice. Each consumer creates one client per config entry and merges results, using the issue's service name to resolve which config to use for per-issue operations.

**Tech Stack:** Go, gopkg.in/yaml.v3, Datadog API client

**Spec:** `docs/superpowers/specs/2026-04-16-multi-datadog-config-design.md`

---

## File Map

- **Modify:** `internal/config/config.go` — New `DatadogConfigs` type with custom `UnmarshalYAML`, helper methods, validation
- **Modify:** `internal/config/config_test.go` — Tests for both formats, helpers, validation, defaults
- **Modify:** `cmd/scan.go` — Multi-client scan, iterate configs for `runScan` and `runScanWithResults`
- **Modify:** `cmd/import.go` — Search across all clients, resolve matching config for URLs
- **Modify:** `cmd/investigate.go` — Resolve config by service for client creation
- **Modify:** `cmd/fix.go` — Resolve config by service for URL building
- **Modify:** `cmd/serve.go` — Create multiple clients, wire multi-client adapter
- **Modify:** `cmd/scan_test.go` — Update config construction to use `DatadogConfigs`
- **Modify:** `cmd/import_test.go` — Update config construction to use `DatadogConfigs`
- **Modify:** `internal/syncer/adapter.go` — Accept a client-resolver function instead of a single client
- **Modify:** `config.example.yml` — Add multi-site example

---

### Task 1: DatadogConfigs type with custom UnmarshalYAML

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

- [ ] **Step 1: Write failing test for flat (single) config format**

Add to `internal/config/config_test.go`:

```go
func TestDatadogConfigs_FlatFormat(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yml")

	content := `
datadog:
  token: "my-token"
  site: "datadoghq.eu"
  org_subdomain: "myorg"
  services:
    - "svc-a"
    - "svc-b"
`
	os.WriteFile(configPath, []byte(content), 0644)

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cfg.Datadog) != 1 {
		t.Fatalf("expected 1 datadog config, got %d", len(cfg.Datadog))
	}
	dd := cfg.Datadog[0]
	if dd.Token != "my-token" {
		t.Errorf("expected token 'my-token', got %q", dd.Token)
	}
	if dd.Site != "datadoghq.eu" {
		t.Errorf("expected site 'datadoghq.eu', got %q", dd.Site)
	}
	if dd.OrgSubdomain != "myorg" {
		t.Errorf("expected org_subdomain 'myorg', got %q", dd.OrgSubdomain)
	}
	if len(dd.Services) != 2 {
		t.Errorf("expected 2 services, got %d", len(dd.Services))
	}
}
```

- [ ] **Step 2: Write failing test for multi-site (named map) config format**

Add to `internal/config/config_test.go`:

```go
func TestDatadogConfigs_MultiFormat(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yml")

	content := `
datadog:
  work:
    token: "work-token"
    site: "datadoghq.eu"
    org_subdomain: "workorg"
    services:
      - "svc-a"
  personal:
    token: "personal-token"
    site: "datadoghq.com"
    services:
      - "svc-x"
`
	os.WriteFile(configPath, []byte(content), 0644)

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cfg.Datadog) != 2 {
		t.Fatalf("expected 2 datadog configs, got %d", len(cfg.Datadog))
	}

	// Find each by name
	var work, personal *DatadogConfig
	for i := range cfg.Datadog {
		switch cfg.Datadog[i].Name {
		case "work":
			work = &cfg.Datadog[i]
		case "personal":
			personal = &cfg.Datadog[i]
		}
	}

	if work == nil || personal == nil {
		t.Fatal("expected both 'work' and 'personal' configs")
	}
	if work.Token != "work-token" {
		t.Errorf("work token: got %q", work.Token)
	}
	if personal.Token != "personal-token" {
		t.Errorf("personal token: got %q", personal.Token)
	}
	if work.Site != "datadoghq.eu" {
		t.Errorf("work site: got %q", work.Site)
	}
	if personal.Site != "datadoghq.com" {
		t.Errorf("personal site: got %q", personal.Site)
	}
	if len(work.Services) != 1 || work.Services[0] != "svc-a" {
		t.Errorf("work services: got %v", work.Services)
	}
	if len(personal.Services) != 1 || personal.Services[0] != "svc-x" {
		t.Errorf("personal services: got %v", personal.Services)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `cd /Users/jorgenbs/dev/ruter/fido && go test ./internal/config/ -run "TestDatadogConfigs" -v`
Expected: compilation errors because `cfg.Datadog` is still `DatadogConfig`, not `DatadogConfigs`

- [ ] **Step 4: Implement DatadogConfigs type with UnmarshalYAML**

In `internal/config/config.go`, change the `Config` struct and add the new type:

```go
type Config struct {
	Datadog      DatadogConfigs            `yaml:"datadog"`
	Scan         ScanConfig                `yaml:"scan"`
	Repositories map[string]RepoConfig     `yaml:"repositories"`
	Agent        AgentConfig               `yaml:"agent"`
}

type DatadogConfig struct {
	Name         string   `yaml:"-"`
	Token        string   `yaml:"token"`
	Site         string   `yaml:"site"`
	OrgSubdomain string   `yaml:"org_subdomain"`
	Services     []string `yaml:"services"`
}

type DatadogConfigs []DatadogConfig

func (d *DatadogConfigs) UnmarshalYAML(value *yaml.Node) error {
	// Try flat format first: check if any child key is "token"
	if value.Kind == yaml.MappingNode {
		for i := 0; i < len(value.Content)-1; i += 2 {
			if value.Content[i].Value == "token" {
				// Flat format — decode as a single DatadogConfig
				var single DatadogConfig
				if err := value.Decode(&single); err != nil {
					return err
				}
				*d = DatadogConfigs{single}
				return nil
			}
		}
	}

	// Multi format — decode as map[string]DatadogConfig
	var m map[string]DatadogConfig
	if err := value.Decode(&m); err != nil {
		return fmt.Errorf("datadog config must be either a single config (with token/site/services) or a map of named configs: %w", err)
	}
	configs := make(DatadogConfigs, 0, len(m))
	for name, cfg := range m {
		cfg.Name = name
		configs = append(configs, cfg)
	}
	*d = configs
	return nil
}
```

Update the `Load` function defaults. The old single default `Datadog: DatadogConfig{Site: "datadoghq.eu", OrgSubdomain: "app"}` can no longer be set as a struct default because `DatadogConfigs` uses custom unmarshaling. Instead, apply defaults after unmarshaling:

```go
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

	// Apply defaults to each Datadog config entry
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
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd /Users/jorgenbs/dev/ruter/fido && go test ./internal/config/ -run "TestDatadogConfigs" -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat: add DatadogConfigs type with custom YAML unmarshaling

Supports both flat single-config and named multi-config formats.
The flat format (with token key) wraps into a one-element slice.
The map format sets each entry's Name from the map key."
```

---

### Task 2: Helper methods and validation on DatadogConfigs

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

- [ ] **Step 1: Write failing tests for AllServices, ForService, and duplicate validation**

Add to `internal/config/config_test.go`:

```go
func TestDatadogConfigs_AllServices(t *testing.T) {
	configs := DatadogConfigs{
		{Name: "a", Services: []string{"svc-1", "svc-2"}},
		{Name: "b", Services: []string{"svc-3"}},
	}
	all := configs.AllServices()
	if len(all) != 3 {
		t.Fatalf("expected 3 services, got %d", len(all))
	}
}

func TestDatadogConfigs_ForService(t *testing.T) {
	configs := DatadogConfigs{
		{Name: "a", Token: "tok-a", Services: []string{"svc-1", "svc-2"}},
		{Name: "b", Token: "tok-b", Services: []string{"svc-3"}},
	}

	got := configs.ForService("svc-3")
	if got == nil {
		t.Fatal("expected config for svc-3")
	}
	if got.Token != "tok-b" {
		t.Errorf("expected tok-b, got %q", got.Token)
	}

	if configs.ForService("nonexistent") != nil {
		t.Error("expected nil for unknown service")
	}
}

func TestDatadogConfigs_ForService_FallbackToFirst(t *testing.T) {
	configs := DatadogConfigs{
		{Name: "a", Token: "tok-a", Services: []string{"svc-1"}},
	}
	// Unknown service should return nil (callers decide fallback)
	if configs.ForService("unknown") != nil {
		t.Error("expected nil for unknown service")
	}
}

func TestLoad_DuplicateServicesAcrossConfigs(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yml")
	content := `
datadog:
  a:
    token: "tok-a"
    site: "datadoghq.eu"
    services: ["svc-overlap"]
  b:
    token: "tok-b"
    site: "datadoghq.com"
    services: ["svc-overlap"]
`
	os.WriteFile(configPath, []byte(content), 0644)

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("expected error for duplicate service across configs")
	}
	if !strings.Contains(err.Error(), "svc-overlap") {
		t.Errorf("expected error mentioning 'svc-overlap', got: %v", err)
	}
}
```

Note: add `"strings"` to the imports in `config_test.go`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/jorgenbs/dev/ruter/fido && go test ./internal/config/ -run "TestDatadogConfigs_AllServices|TestDatadogConfigs_ForService|TestLoad_Duplicate" -v`
Expected: FAIL — methods don't exist yet

- [ ] **Step 3: Implement AllServices, ForService, and validation**

Add to `internal/config/config.go`:

```go
// AllServices returns a flattened list of all services across all Datadog configs.
func (d DatadogConfigs) AllServices() []string {
	var all []string
	for _, cfg := range d {
		all = append(all, cfg.Services...)
	}
	return all
}

// ForService returns the DatadogConfig that owns the given service name.
// Returns nil if no config contains the service.
func (d DatadogConfigs) ForService(service string) *DatadogConfig {
	for i := range d {
		for _, s := range d[i].Services {
			if s == service {
				return &d[i]
			}
		}
	}
	return nil
}
```

Add validation in `Load`, after the defaults loop:

```go
	// Validate: no duplicate services across configs
	seen := map[string]string{} // service -> config name
	for _, dd := range cfg.Datadog {
		for _, svc := range dd.Services {
			if prev, ok := seen[svc]; ok {
				name := dd.Name
				if name == "" {
					name = "(default)"
				}
				return nil, fmt.Errorf("service %q appears in multiple datadog configs: %q and %q", svc, prev, name)
			}
			seen[svc] = dd.Name
		}
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/jorgenbs/dev/ruter/fido && go test ./internal/config/ -v`
Expected: PASS (all config tests, including old ones that now need updating — see next step)

- [ ] **Step 5: Fix existing config tests for new type**

The existing tests reference `cfg.Datadog.Token`, `cfg.Datadog.Services`, `cfg.Datadog.Site` — these need updating to use the slice. Update `TestLoadConfig`:

```go
func TestLoadConfig(t *testing.T) {
	// ... same YAML content ...

	if len(cfg.Datadog) != 1 {
		t.Fatalf("expected 1 datadog config, got %d", len(cfg.Datadog))
	}
	if cfg.Datadog[0].Token != "test-pat-token" {
		t.Errorf("expected token 'test-pat-token', got %q", cfg.Datadog[0].Token)
	}
	if len(cfg.Datadog[0].Services) != 2 {
		t.Errorf("expected 2 services, got %d", len(cfg.Datadog[0].Services))
	}
	// ... rest unchanged ...
}
```

Update `TestLoadConfig_DefaultValues`:

```go
	if len(cfg.Datadog) != 1 {
		t.Fatalf("expected 1 datadog config, got %d", len(cfg.Datadog))
	}
	if cfg.Datadog[0].Site != "datadoghq.eu" {
		t.Errorf("expected default site 'datadoghq.eu', got %q", cfg.Datadog[0].Site)
	}
```

- [ ] **Step 6: Run all config tests**

Run: `cd /Users/jorgenbs/dev/ruter/fido && go test ./internal/config/ -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat: add AllServices, ForService helpers and duplicate service validation"
```

---

### Task 3: Update cmd/scan.go for multi-config

**Files:**
- Modify: `cmd/scan.go`
- Modify: `cmd/scan_test.go`

- [ ] **Step 1: Update scan_test.go config construction**

Every test that constructs `config.Config{Datadog: config.DatadogConfig{...}}` must change to use the slice. Update all test configs in `cmd/scan_test.go`:

Replace every occurrence of:
```go
Datadog: config.DatadogConfig{
    Services:     []string{"svc-a"},
    Site:         "test.datadoghq.com",
    OrgSubdomain: "myorg",
},
```

With:
```go
Datadog: config.DatadogConfigs{{
    Services:     []string{"svc-a"},
    Site:         "test.datadoghq.com",
    OrgSubdomain: "myorg",
}},
```

And simpler ones like `Datadog: config.DatadogConfig{Services: []string{"svc-a"}}` become `Datadog: config.DatadogConfigs{{Services: []string{"svc-a"}}}`.

- [ ] **Step 2: Update runScan and runScanWithResults**

In `cmd/scan.go`, `runScan` and `runScanWithResults` currently call `cfg.Datadog.Services`, `cfg.Datadog.OrgSubdomain`, `cfg.Datadog.Site`. Since these functions receive a single `*datadog.Client`, they operate on one client at a time. The key change is in how they access config fields.

Change the function signatures to accept a `*config.DatadogConfig` for the Datadog-specific fields instead of reading from `cfg.Datadog`:

```go
func runScan(cfg *config.Config, ddCfg *config.DatadogConfig, ddClient *datadog.Client, mgr *reports.Manager) (int, error) {
	issues, err := ddClient.SearchErrorIssues(ddCfg.Services, cfg.Scan.Since)
	// ... replace cfg.Datadog.OrgSubdomain with ddCfg.OrgSubdomain
	// ... replace cfg.Datadog.Site with ddCfg.Site
	// ... rest same
}

func runScanWithResults(cfg *config.Config, ddCfg *config.DatadogConfig, ddClient *datadog.Client, mgr *reports.Manager) (int, []syncer.ScanResult, error) {
	issues, err := ddClient.SearchErrorIssues(ddCfg.Services, cfg.Scan.Since)
	// ... replace cfg.Datadog.OrgSubdomain with ddCfg.OrgSubdomain
	// ... replace cfg.Datadog.Site with ddCfg.Site
	// ... rest same
}
```

- [ ] **Step 3: Update the scanCmd RunE to iterate over configs**

Replace the scanCmd RunE body:

```go
RunE: func(cmd *cobra.Command, args []string) error {
    home, _ := os.UserHomeDir()
    reportsDir := filepath.Join(home, ".fido", "reports")
    mgr := reports.NewManager(reportsDir)

    services, _ := cmd.Flags().GetStringSlice("service")
    since, _ := cmd.Flags().GetString("since")
    if since == "" {
        since = cfg.Scan.Since
    }

    totalCount := 0
    for i := range cfg.Datadog {
        ddCfg := &cfg.Datadog[i]

        // If --service flag given, skip configs that don't own any of the requested services
        cfgServices := ddCfg.Services
        if len(services) > 0 {
            cfgServices = filterServices(ddCfg.Services, services)
            if len(cfgServices) == 0 {
                continue
            }
        }

        ddClient, err := datadog.NewClient(ddCfg.Token, ddCfg.Site, ddCfg.OrgSubdomain)
        if err != nil {
            return err
        }
        ddClient.SetVerbose(verbose)

        scanDdCfg := &config.DatadogConfig{
            Services:     cfgServices,
            Site:         ddCfg.Site,
            OrgSubdomain: ddCfg.OrgSubdomain,
        }
        scanCfg := &config.Config{
            Scan:         config.ScanConfig{Since: since},
            Repositories: cfg.Repositories,
        }

        count, err := runScan(scanCfg, scanDdCfg, ddClient, mgr)
        if err != nil {
            return err
        }
        totalCount += count
    }
    fmt.Printf("Updated %d existing issues\n", totalCount)
    return nil
},
```

Add the `filterServices` helper:

```go
func filterServices(configServices, requestedServices []string) []string {
	requested := map[string]bool{}
	for _, s := range requestedServices {
		requested[s] = true
	}
	var result []string
	for _, s := range configServices {
		if requested[s] {
			result = append(result, s)
		}
	}
	return result
}
```

- [ ] **Step 4: Update test calls to match new signatures**

In `cmd/scan_test.go`, update all calls to `runScan` to pass the extra `ddCfg` parameter:

```go
// Old:
count, err := runScan(cfg, ddClient, mgr)

// New:
count, err := runScan(cfg, &cfg.Datadog[0], ddClient, mgr)
```

Same for `runScanWithResults` calls if any exist in tests.

- [ ] **Step 5: Run scan tests**

Run: `cd /Users/jorgenbs/dev/ruter/fido && go test ./cmd/ -run "TestScan|TestBuild" -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add cmd/scan.go cmd/scan_test.go
git commit -m "refactor: update scan command for multi-Datadog config

runScan and runScanWithResults now accept a DatadogConfig parameter.
The scan command iterates over all configured Datadog entries.
The --service flag filters to configs that own the requested services."
```

---

### Task 4: Update cmd/import.go for multi-config

**Files:**
- Modify: `cmd/import.go`
- Modify: `cmd/import_test.go`

- [ ] **Step 1: Update import_test.go config construction**

Replace every `Datadog: config.DatadogConfig{...}` with `Datadog: config.DatadogConfigs{{...}}` in `cmd/import_test.go` (same pattern as Task 3).

- [ ] **Step 2: Update runImport to search across all configs**

Change `runImport` signature to accept configs and a client-creation function, or simpler: just change how it creates clients. The simplest approach: iterate over `cfg.Datadog`, create a client for each, search, and use the first match.

```go
func runImport(issueID string, cfg *config.Config, ddClients []*datadog.Client, mgr *reports.Manager) error {
	if mgr.Exists(issueID) {
		return fmt.Errorf("issue %s is already imported", issueID)
	}

	var found *datadog.ErrorIssue
	var matchedCfg *config.DatadogConfig

	for i, ddClient := range ddClients {
		issues, err := ddClient.SearchErrorIssues(cfg.Datadog[i].Services, "8760h")
		if err != nil {
			return fmt.Errorf("searching Datadog (%s): %w", cfg.Datadog[i].Name, err)
		}
		for j := range issues {
			if issues[j].ID == issueID {
				found = &issues[j]
				matchedCfg = &cfg.Datadog[i]
				break
			}
		}
		if found != nil {
			break
		}
	}

	if found == nil {
		return fmt.Errorf("issue %s not found on Datadog (searched services: %v)", issueID, cfg.Datadog.AllServices())
	}

	service := found.Attributes.Service
	if _, ok := cfg.Repositories[service]; !ok {
		return fmt.Errorf("service %q is not configured in repositories — add it to your config.yml", service)
	}

	eventsURL := buildEventsURL(matchedCfg.OrgSubdomain, matchedCfg.Site, found.Attributes.Service, found.Attributes.Env, found.Attributes.FirstSeen, found.Attributes.LastSeen)
	tracesURL := buildTracesURL(matchedCfg.OrgSubdomain, matchedCfg.Site, found.Attributes.Service, found.Attributes.Env, found.Attributes.FirstSeen, found.Attributes.LastSeen)
	datadogURL := fmt.Sprintf("https://%s.%s/error-tracking/issue/%s", matchedCfg.OrgSubdomain, matchedCfg.Site, found.ID)

	// ... rest of the function unchanged (uses datadogURL, eventsURL, tracesURL) ...
}
```

- [ ] **Step 3: Update importCmd RunE to create client slice**

```go
RunE: func(cmd *cobra.Command, args []string) error {
    issueID := args[0]
    home, _ := os.UserHomeDir()
    reportsDir := filepath.Join(home, ".fido", "reports")
    mgr := reports.NewManager(reportsDir)

    var ddClients []*datadog.Client
    for i := range cfg.Datadog {
        c, err := datadog.NewClient(cfg.Datadog[i].Token, cfg.Datadog[i].Site, cfg.Datadog[i].OrgSubdomain)
        if err != nil {
            return err
        }
        c.SetVerbose(verbose)
        ddClients = append(ddClients, c)
    }

    if err := runImport(issueID, cfg, ddClients, mgr); err != nil {
        return err
    }
    fmt.Printf("Successfully imported issue %s\n", issueID)
    return nil
},
```

- [ ] **Step 4: Update test calls to pass client slice**

In `cmd/import_test.go`, update calls:

```go
// Old:
err := runImport("issue-abc", cfg, ddClient, mgr)

// New:
err := runImport("issue-abc", cfg, []*datadog.Client{ddClient}, mgr)
```

- [ ] **Step 5: Run import tests**

Run: `cd /Users/jorgenbs/dev/ruter/fido && go test ./cmd/ -run "TestImport" -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add cmd/import.go cmd/import_test.go
git commit -m "refactor: update import command for multi-Datadog config

runImport now accepts a slice of clients and searches across all
configured Datadog sites. Uses the matching config for URL building."
```

---

### Task 5: Update cmd/investigate.go and cmd/fix.go for multi-config

**Files:**
- Modify: `cmd/investigate.go`
- Modify: `cmd/fix.go`

- [ ] **Step 1: Update investigate.go client creation**

In the `investigateCmd` RunE, replace the single-client creation with service-based config lookup:

```go
var ddClient *datadog.Client
ddCfg := cfg.Datadog.ForService(service)
if ddCfg == nil && len(cfg.Datadog) > 0 {
    ddCfg = &cfg.Datadog[0]
}
if ddCfg != nil && ddCfg.Token != "" {
    if c, err := datadog.NewClient(ddCfg.Token, ddCfg.Site, ddCfg.OrgSubdomain); err == nil {
        c.SetVerbose(verbose)
        ddClient = c
    }
}
```

This replaces the existing lines 47-52 that directly read `cfg.Datadog.Token`, `cfg.Datadog.Site`, `cfg.Datadog.OrgSubdomain`.

- [ ] **Step 2: Update fix.go URL building**

In `cmd/fix.go`, the `runFix` function builds a Datadog URL on line 130:

```go
datadogURL := fmt.Sprintf("https://%s.%s/error-tracking/issue/%s", cfg.Datadog.OrgSubdomain, cfg.Datadog.Site, issueID)
```

This needs to resolve the config by service. Add a `ddCfg` parameter to `runFix`:

Change the function signature and the caller at line 75 in serve.go.

Actually, simpler approach: resolve inside `runFix` using the service parameter that's already passed in:

```go
func runFix(issueID, service string, cfg *config.Config, mgr *reports.Manager, progress io.Writer) error {
    // ... existing error/investigation reading ...

    ddCfg := cfg.Datadog.ForService(service)
    if ddCfg == nil && len(cfg.Datadog) > 0 {
        ddCfg = &cfg.Datadog[0]
    }

    orgSubdomain := "app"
    site := "datadoghq.eu"
    if ddCfg != nil {
        orgSubdomain = ddCfg.OrgSubdomain
        site = ddCfg.Site
    }

    home, _ := os.UserHomeDir()
    issueReportsDir := filepath.Join(home, ".fido", "reports", issueID)
    datadogURL := fmt.Sprintf("https://%s.%s/error-tracking/issue/%s", orgSubdomain, site, issueID)

    // ... rest unchanged ...
}
```

- [ ] **Step 3: Verify compilation**

Run: `cd /Users/jorgenbs/dev/ruter/fido && go build ./...`
Expected: Success (may still fail due to serve.go — addressed in Task 6)

- [ ] **Step 4: Commit**

```bash
git add cmd/investigate.go cmd/fix.go
git commit -m "refactor: update investigate and fix commands for multi-Datadog config

Both commands now resolve the correct Datadog config by service name,
falling back to the first config entry if the service is unknown."
```

---

### Task 6: Update cmd/serve.go and syncer adapter for multi-config

**Files:**
- Modify: `cmd/serve.go`
- Modify: `internal/syncer/adapter.go`

- [ ] **Step 1: Update the syncer adapter to support multiple clients**

Change `Adapter` to use a client-resolver function instead of a single client. In `internal/syncer/adapter.go`:

```go
type ClientResolver func(service string) *datadog.Client

type Adapter struct {
	resolveClient ClientResolver
	defaultClient *datadog.Client
	mgr           *reports.Manager
	hub           *api.Hub
	scanFn        func() ([]ScanResult, error)
}

func NewAdapter(
	resolveClient ClientResolver,
	defaultClient *datadog.Client,
	mgr *reports.Manager,
	hub *api.Hub,
	scanFn func() ([]ScanResult, error),
) *Adapter {
	return &Adapter{
		resolveClient: resolveClient,
		defaultClient: defaultClient,
		mgr:           mgr,
		hub:           hub,
		scanFn:        scanFn,
	}
}
```

Update `FetchStacktrace` to resolve the client by service:

```go
func (a *Adapter) FetchStacktrace(issueID, service, env, firstSeen, lastSeen string) (string, error) {
	if service == "" || firstSeen == "" || lastSeen == "" {
		meta, err := a.mgr.ReadMetadata(issueID)
		if err != nil {
			return "", fmt.Errorf("reading metadata for %s: %w", issueID, err)
		}
		if service == "" {
			service = meta.Service
		}
		if env == "" {
			env = meta.Env
		}
		if firstSeen == "" {
			firstSeen = meta.FirstSeen
		}
		if lastSeen == "" {
			lastSeen = meta.LastSeen
		}
	}

	client := a.resolveClient(service)
	if client == nil {
		client = a.defaultClient
	}
	ctx, err := client.FetchIssueContext(issueID, service, env, firstSeen, lastSeen)
	if err != nil {
		return "", err
	}
	return ctx.StackTrace, nil
}
```

Update `ResolveIssue` and `GetIssueStatus` to use the default client (these operate on Datadog issue IDs, not service-scoped — we'd need the service from the tracked issue's metadata):

```go
func (a *Adapter) ResolveIssue(datadogIssueID string) error {
	return a.defaultClient.ResolveIssue(datadogIssueID)
}

func (a *Adapter) GetIssueStatus(datadogIssueID string) (string, error) {
	return a.defaultClient.GetIssueStatus(datadogIssueID)
}
```

Wait — for `ResolveIssue` and `GetIssueStatus`, we need the correct client for the issue. The adapter has access to `mgr`, so it can look up the service from metadata and resolve the client:

```go
func (a *Adapter) resolveClientForIssue(issueID string) *datadog.Client {
	if meta, err := a.mgr.ReadMetadata(issueID); err == nil && meta.Service != "" {
		if c := a.resolveClient(meta.Service); c != nil {
			return c
		}
	}
	return a.defaultClient
}

func (a *Adapter) ResolveIssue(datadogIssueID string) error {
	// datadogIssueID is also the issue ID in our reports
	client := a.resolveClientForIssue(datadogIssueID)
	return client.ResolveIssue(datadogIssueID)
}

func (a *Adapter) GetIssueStatus(datadogIssueID string) (string, error) {
	client := a.resolveClientForIssue(datadogIssueID)
	return client.GetIssueStatus(datadogIssueID)
}
```

- [ ] **Step 2: Update serve.go to create multiple clients and a resolver**

In `cmd/serve.go`, replace the single client creation with a multi-client setup:

```go
RunE: func(cmd *cobra.Command, args []string) error {
    port, _ := cmd.Flags().GetString("port")
    home, _ := os.UserHomeDir()
    reportsDir := filepath.Join(home, ".fido", "reports")
    mgr := reports.NewManager(reportsDir)

    // Create a client per Datadog config
    clientMap := map[string]*datadog.Client{} // service -> client
    var defaultClient *datadog.Client
    for i := range cfg.Datadog {
        ddCfg := &cfg.Datadog[i]
        c, err := datadog.NewClient(ddCfg.Token, ddCfg.Site, ddCfg.OrgSubdomain)
        if err != nil {
            return err
        }
        c.SetVerbose(verbose)
        if defaultClient == nil {
            defaultClient = c
        }
        for _, svc := range ddCfg.Services {
            clientMap[svc] = c
        }
    }
    if defaultClient == nil {
        return fmt.Errorf("at least one datadog config is required")
    }

    resolveClient := func(service string) *datadog.Client {
        if c, ok := clientMap[service]; ok {
            return c
        }
        return defaultClient
    }

    hub := api.NewHub()
    server := api.NewServer(mgr, cfg, hub)
    handlers := api.GetHandlers(server)

    handlers.SetScanFunc(func() error {
        totalCount := 0
        for i := range cfg.Datadog {
            ddCfg := &cfg.Datadog[i]
            if len(ddCfg.Services) == 0 {
                continue
            }
            client := resolveClient(ddCfg.Services[0])
            count, err := runScan(cfg, ddCfg, client, mgr)
            if err != nil {
                return err
            }
            totalCount += count
        }
        fmt.Printf("API scan complete: %d new issues\n", totalCount)
        return nil
    })

    handlers.SetInvestigateFunc(func(issueID string, progress io.Writer) error {
        service := ""
        if meta, err := mgr.ReadMetadata(issueID); err == nil {
            service = meta.Service
        }
        if service == "" {
            errorContent, _ := mgr.ReadError(issueID)
            service = extractServiceFromReport(errorContent)
        }
        ddClient := resolveClient(service)
        return runInvestigate(issueID, service, cfg, mgr, ddClient, progress)
    })

    handlers.SetFixFunc(func(issueID string, iterate bool, progress io.Writer) error {
        service := ""
        if meta, err := mgr.ReadMetadata(issueID); err == nil {
            service = meta.Service
        }
        if service == "" {
            errorContent, _ := mgr.ReadError(issueID)
            service = extractServiceFromReport(errorContent)
        }
        if iterate {
            return runFixIterate(issueID, service, cfg, mgr, progress)
        }
        return runFix(issueID, service, cfg, mgr, progress)
    })

    // ... interval/rateLimit parsing unchanged ...

    // Run initial scan across all configs
    fmt.Println("Running initial scan...")
    totalCount := 0
    var allResults []syncer.ScanResult
    for i := range cfg.Datadog {
        ddCfg := &cfg.Datadog[i]
        if len(ddCfg.Services) == 0 {
            continue
        }
        client := resolveClient(ddCfg.Services[0])
        count, results, scanErr := runScanWithResults(cfg, ddCfg, client, mgr)
        if scanErr != nil {
            return fmt.Errorf("initial scan failed (check your Datadog token for %q): %w", ddCfg.Name, scanErr)
        }
        totalCount += count
        allResults = append(allResults, results...)
    }
    fmt.Printf("Initial scan complete: %d issues updated\n", totalCount)
    hub.Publish(api.Event{Type: "scan:complete", Payload: map[string]any{"count": totalCount}})

    adapter := syncer.NewAdapter(resolveClient, defaultClient, mgr, hub, func() ([]syncer.ScanResult, error) {
        var all []syncer.ScanResult
        for i := range cfg.Datadog {
            ddCfg := &cfg.Datadog[i]
            if len(ddCfg.Services) == 0 {
                continue
            }
            client := resolveClient(ddCfg.Services[0])
            c, results, err := runScanWithResults(cfg, ddCfg, client, mgr)
            if err != nil {
                return nil, err
            }
            fmt.Printf("Background scan complete (%s): %d issues updated\n", ddCfg.Name, c)
            all = append(all, results...)
        }
        return all, nil
    })

    // ... engine, import handler, rate limit callback, ctx, server — unchanged ...
    // Note: rate limit callback should be set on ALL clients
```

For the rate limit callback, set it on all unique clients (deduplicated since multiple services may share a client):

```go
    limiter := engine.Limiter()
    seen := map[*datadog.Client]bool{}
    for _, client := range clientMap {
        if seen[client] {
            continue
        }
        seen[client] = true
        client.SetRateLimitCallback(func(info datadog.RateLimitInfo) {
            limiter.Update(info.Limit, info.Remaining, time.Duration(info.Period)*time.Second, info.Reset)
        })
    }
```

For the import handler, update to pass client slice:

```go
    handlers.SetImportFunc(func(issueID string) error {
        var ddClients []*datadog.Client
        for i := range cfg.Datadog {
            ddClients = append(ddClients, resolveClient(cfg.Datadog[i].Services[0]))
        }
        if err := runImport(issueID, cfg, ddClients, mgr); err != nil {
            return err
        }
        meta, _ := mgr.ReadMetadata(issueID)
        if meta != nil {
            engine.EnqueueIssue(syncer.ScanResult{
                IssueID:   issueID,
                Service:   meta.Service,
                Env:       meta.Env,
                FirstSeen: meta.FirstSeen,
                LastSeen:  meta.LastSeen,
            })
        }
        return nil
    })
```

- [ ] **Step 3: Verify full compilation**

Run: `cd /Users/jorgenbs/dev/ruter/fido && go build ./...`
Expected: Success

- [ ] **Step 4: Run all tests**

Run: `cd /Users/jorgenbs/dev/ruter/fido && go test ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/serve.go internal/syncer/adapter.go
git commit -m "refactor: update serve command and syncer adapter for multi-Datadog config

The adapter now uses a ClientResolver function to route Datadog API
calls to the correct client based on service name. serve.go creates
a client per config entry and iterates all configs for scans."
```

---

### Task 7: Update config.example.yml and final verification

**Files:**
- Modify: `config.example.yml`

- [ ] **Step 1: Add multi-site example to config.example.yml**

Add the multi-site format as a commented-out alternative in `config.example.yml`, below the existing single-site config:

```yaml
# Fido configuration
# Copy to ~/.fido/config.yml and fill in your values.

datadog:
  token: ""         # Datadog Personal Access Token (https://app.datadoghq.eu/personal-settings/personal-access-tokens)
                    # Required scopes: error_tracking_read, apm_read
                    # (apm_read is needed to fetch stack traces via the Spans API)
  site: "datadoghq.eu"   # Datadog site — NOT your org subdomain (e.g. use "datadoghq.eu", not "myorg.datadoghq.eu")
  services:         # Datadog service names to monitor
    - "my-service"

# Multi-site alternative — use named entries instead of a flat config:
# datadog:
#   work:
#     token: ""
#     site: "datadoghq.eu"
#     org_subdomain: "myworkorg"
#     services:
#       - "svc-a"
#       - "svc-b"
#   personal:
#     token: ""
#     site: "datadoghq.com"
#     services:
#       - "svc-x"

scan:
  interval: "15m"   # How often the daemon polls Datadog
  since: "24h"      # How far back to look for errors
  rate_limit: 30              # Max Datadog API requests per minute

# Map Datadog service names to code repositories.
# Use "local" for a filesystem path, or "git" for a clone URL.
repositories:
  my-service:
    local: "/path/to/my-service"
  # another-service:
  #   git: "https://gitlab.com/org/another-service.git"

agent:
  # --dangerously-skip-permissions avoids tool-call permission prompts when running non-interactively.
  investigate: "claude -p --dangerously-skip-permissions --output-format stream-json --verbose"
  fix: "claude"
```

- [ ] **Step 2: Run full test suite**

Run: `cd /Users/jorgenbs/dev/ruter/fido && go test ./...`
Expected: PASS

- [ ] **Step 3: Build and verify binary starts**

Run: `cd /Users/jorgenbs/dev/ruter/fido && go build -o fido .`
Expected: Success

- [ ] **Step 4: Commit**

```bash
git add config.example.yml
git commit -m "docs: add multi-site Datadog config example to config.example.yml"
```
