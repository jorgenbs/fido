# Multi-Datadog Config

**Date:** 2026-04-16

## Summary

Expand the Fido config to support multiple Datadog sites/orgs, each with its own token, site, org_subdomain, and services list. The existing single-config format remains valid — backward compatibility is preserved via custom YAML unmarshaling.

## Config Format

### Single-site (existing, still works)

```yaml
datadog:
  token: "..."
  site: "datadoghq.eu"
  org_subdomain: "app"
  services:
    - "svc-a"
    - "svc-b"
```

### Multi-site (new)

```yaml
datadog:
  ruter:
    token: "..."
    site: "datadoghq.eu"
    org_subdomain: "ruter"
    services:
      - "svc-a"
      - "svc-b"
  personal:
    token: "..."
    site: "datadoghq.com"
    services:
      - "svc-x"
```

The map key (`ruter`, `personal`) becomes the `Name` field on `DatadogConfig`. It is for human readability only — not used in matching or logic.

### Detection logic

Custom `UnmarshalYAML` on a new `DatadogConfigs` type (a `[]DatadogConfig`):

1. Attempt to decode the YAML node as a flat `DatadogConfig` (check for `token` key presence).
2. If that succeeds → wrap in a one-element slice with `Name: ""`.
3. Otherwise → decode as `map[string]DatadogConfig`, set each entry's `Name` from the map key, collect into a slice.

## Struct Changes

### `internal/config/config.go`

```go
type Config struct {
    Datadog      DatadogConfigs            `yaml:"datadog"`
    Scan         ScanConfig                `yaml:"scan"`
    Repositories map[string]RepoConfig     `yaml:"repositories"`
    Agent        AgentConfig               `yaml:"agent"`
}

type DatadogConfig struct {
    Name         string   `yaml:"-"`              // set from map key, not serialized
    Token        string   `yaml:"token"`
    Site         string   `yaml:"site"`
    OrgSubdomain string   `yaml:"org_subdomain"`
    Services     []string `yaml:"services"`
}

type DatadogConfigs []DatadogConfig

func (d *DatadogConfigs) UnmarshalYAML(value *yaml.Node) error {
    // 1. Try flat single-config (has "token" key)
    // 2. Fall back to map[string]DatadogConfig
}
```

### Helper methods on `DatadogConfigs`

- `AllServices() []string` — flattened list of all services across all configs.
- `ForService(service string) *DatadogConfig` — returns the config that owns the given service. Returns nil if not found.

## Consumer Changes

### `cmd/serve.go`

Create a `[]*datadog.Client` — one per `DatadogConfig` entry. The scan function iterates all clients, merging results. The adapter wraps all clients.

### `cmd/scan.go`

Create one client per config entry. `runScan` / `runScanWithResults` accept the full config and a slice of clients. Each client searches its own services. Results are merged.

The `--service` flag override still works: filter to only the config(s) whose services list intersects the flag value.

### `cmd/import.go`

Search across all clients. Each client searches its own services with a wide time window. Return the first match. Use the matching config's site/org_subdomain for URL building.

### `cmd/investigate.go`

Look up the issue's service from `meta.json`, call `cfg.Datadog.ForService(service)` to find the right config, create a client from that config. Falls back to first config if service is unknown.

### `cmd/fix.go`

Same pattern as investigate — resolve config via service from meta.json.

### `cmd/scan.go` URL building

`buildEventsURL` / `buildTracesURL` / datadog URL already take `orgSubdomain` and `site` as parameters. These are sourced from the matching config entry rather than the single `cfg.Datadog`.

## What Doesn't Change

- `internal/datadog/client.go` — remains single-site, no changes needed.
- `internal/syncer/engine.go` — the `Deps` interface stays the same; the adapter handles multi-client.
- `internal/reports/` — no schema changes. `meta.json` already stores `service`, which is the lookup key.
- `internal/api/` — handlers don't interact with Datadog config directly.
- Frontend — unchanged.

## Config Loading Defaults

When using the flat format, the existing defaults apply (`site: "datadoghq.eu"`, `org_subdomain: "app"`). For the multi-format, defaults apply per entry in the same way.

## Validation

`config.Load` validates after unmarshaling:
- At least one Datadog config entry must exist.
- Each entry must have a non-empty `token` and `site`.
- No duplicate service names across entries (since each service belongs to exactly one site).

## Testing

- `config_test.go`: Test both flat and map YAML formats unmarshal correctly. Test `ForService` and `AllServices` helpers. Test duplicate service validation.
- Existing `scan_test.go`, `import_test.go`: Update test configs to use new `DatadogConfigs` type (wrapping single config in a slice).
