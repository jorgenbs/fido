package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DataDog/datadog-api-client-go/v2/api/datadog"
	ddclient "github.com/jorgenbs/fido/internal/datadog"
	"github.com/jorgenbs/fido/internal/config"
	"github.com/jorgenbs/fido/internal/reports"
)

func newTestDDClient(t *testing.T, serverURL string) *ddclient.Client {
	t.Helper()
	c, err := ddclient.NewClient("token", "test.datadoghq.com", "myorg")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	c.OverrideServers(datadog.ServerConfigurations{
		{URL: serverURL, Description: "test"},
	})
	return c
}

func newTestScanServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "spans") {
			// Spans API: return empty result (FetchIssueContext is non-fatal)
			json.NewEncoder(w).Encode(map[string]interface{}{"data": []interface{}{}})
			return
		}
		// Error issues search API
		resp := map[string]interface{}{
			"data": []map[string]interface{}{
				{
					"id":   "issue-1",
					"type": "issues_search_result",
					"attributes": map[string]interface{}{
						"total_count": 10,
					},
				},
			},
			"included": []map[string]interface{}{
				{
					"id":   "issue-1",
					"type": "issue",
					"attributes": map[string]interface{}{
						"error_type":    "NullPointerException",
						"error_message": "null in handler",
						"service":       "svc-a",
						"state":         "OPEN",
						"first_seen":    int64(1711324800000),
						"last_seen":     int64(1711411200000),
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
}

func TestScanCommand_DoesNotCreateNewIssues(t *testing.T) {
	server := newTestScanServer(t)
	defer server.Close()

	reportsDir := t.TempDir()
	mgr := reports.NewManager(reportsDir)
	ddClient := newTestDDClient(t, server.URL)

	cfg := &config.Config{
		Datadog: config.DatadogConfigs{{
			Services:     []string{"svc-a"},
			Site:         "test.datadoghq.com",
			OrgSubdomain: "myorg",
		}},
		Scan: config.ScanConfig{Since: "24h"},
	}

	count, err := runScan(cfg, &cfg.Datadog[0], ddClient, mgr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 new issues (scan no longer creates), got %d", count)
	}

	if mgr.Exists("issue-1") {
		t.Error("expected issue-1 report to NOT exist (scan no longer creates)")
	}
}

func TestScanCommand_UpdatesExistingIssues(t *testing.T) {
	server := newTestScanServer(t)
	defer server.Close()

	reportsDir := t.TempDir()
	mgr := reports.NewManager(reportsDir)
	ddClient := newTestDDClient(t, server.URL)

	// Pre-create the issue with stale metadata
	mgr.WriteError("issue-1", "existing error report")
	mgr.WriteMetadata("issue-1", &reports.MetaData{
		Service: "svc-a",
		Title:   "OldTitle",
		Count:   1,
	})

	cfg := &config.Config{
		Datadog: config.DatadogConfigs{{
			Services:     []string{"svc-a"},
			Site:         "test.datadoghq.com",
			OrgSubdomain: "myorg",
		}},
		Scan: config.ScanConfig{Since: "24h"},
	}

	_, err := runScan(cfg, &cfg.Datadog[0], ddClient, mgr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	meta, err := mgr.ReadMetadata("issue-1")
	if err != nil {
		t.Fatalf("ReadMetadata: %v", err)
	}
	if meta.Title != "NullPointerException" {
		t.Errorf("expected title updated to NullPointerException, got %q", meta.Title)
	}
	if meta.Count != 10 {
		t.Errorf("expected count updated to 10, got %d", meta.Count)
	}
}

func TestScanCommand_SkipsExistingIssues(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"data": []map[string]interface{}{
				{
					"id":   "issue-1",
					"type": "issues_search_result",
					"attributes": map[string]interface{}{
						"total_count": 1,
					},
				},
			},
			"included": []map[string]interface{}{
				{
					"id":   "issue-1",
					"type": "issue",
					"attributes": map[string]interface{}{
						"error_type": "Err",
						"service":    "svc-a",
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	reportsDir := t.TempDir()
	mgr := reports.NewManager(reportsDir)
	ddClient := newTestDDClient(t, server.URL)

	mgr.WriteError("issue-1", "existing report")
	mgr.WriteMetadata("issue-1", &reports.MetaData{Service: "svc-a"})

	cfg := &config.Config{
		Datadog: config.DatadogConfigs{{Services: []string{"svc-a"}}},
		Scan:    config.ScanConfig{Since: "24h"},
	}

	count, err := runScan(cfg, &cfg.Datadog[0], ddClient, mgr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 updated issue, got %d", count)
	}
}

func TestBuildEventsURL_EncodesQuery(t *testing.T) {
	u := buildEventsURL("myorg", "datadoghq.eu", "my-service", "prod", "2025-01-01T00:00:00Z", "2025-01-02T00:00:00Z")
	if u == "" {
		t.Fatal("expected non-empty URL")
	}
	if strings.Contains(u, "query=service:my-service env:") {
		t.Error("query parameter contains unescaped space")
	}
	if !strings.Contains(u, "query=") {
		t.Error("expected query= in URL")
	}
	if !strings.Contains(u, "my-service") {
		t.Error("expected service name in URL")
	}
	if !strings.Contains(u, "prod") {
		t.Error("expected env in URL")
	}
}

func TestBuildEventsURL_EmptyEnv(t *testing.T) {
	u := buildEventsURL("myorg", "datadoghq.eu", "my-service", "", "2025-01-01T00:00:00Z", "2025-01-02T00:00:00Z")
	if u == "" {
		t.Fatal("expected non-empty URL")
	}
	if strings.Contains(u, "env:") {
		t.Error("URL should omit env: when env is empty")
	}
}

func TestBuildTracesURL_EncodesQuery(t *testing.T) {
	u := buildTracesURL("myorg", "datadoghq.eu", "my-service", "prod", "2025-01-01T00:00:00Z", "2025-01-02T00:00:00Z")
	if u == "" {
		t.Fatal("expected non-empty URL")
	}
	if strings.Contains(u, "query=service:my-service env:") {
		t.Error("query parameter contains unescaped space")
	}
}

func TestBuildTracesURL_EmptyEnv(t *testing.T) {
	u := buildTracesURL("myorg", "datadoghq.eu", "my-service", "", "2025-01-01T00:00:00Z", "2025-01-02T00:00:00Z")
	if u == "" {
		t.Fatal("expected non-empty URL")
	}
	if strings.Contains(u, "env:") {
		t.Error("URL should omit env: when env is empty")
	}
}

func TestRunScan_CIRefresh_SkipsWhenNoResolve(t *testing.T) {
	// An issue with no resolve.json should not cause any CI-related errors.
	server := newTestScanServer(t)
	defer server.Close()

	reportsDir := t.TempDir()
	mgr := reports.NewManager(reportsDir)
	// Pre-create issue so scan skips it (already exists)
	mgr.WriteError("issue-1", "existing")
	mgr.WriteMetadata("issue-1", &reports.MetaData{Service: "svc-a"})
	// No resolve.json → CI refresh should be a no-op

	ddClient := newTestDDClient(t, server.URL)
	cfg := &config.Config{
		Datadog: config.DatadogConfigs{{Services: []string{"svc-a"}}},
		Scan:    config.ScanConfig{Since: "24h"},
	}

	_, err := runScan(cfg, &cfg.Datadog[0], ddClient, mgr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// meta.json should still have empty CIStatus
	meta, _ := mgr.ReadMetadata("issue-1")
	if meta.CIStatus != "" {
		t.Errorf("expected empty CIStatus, got %q", meta.CIStatus)
	}
}
