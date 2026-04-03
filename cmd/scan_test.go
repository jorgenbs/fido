package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DataDog/datadog-api-client-go/v2/api/datadog"
	ddclient "github.com/ruter-as/fido/internal/datadog"
	"github.com/ruter-as/fido/internal/config"
	"github.com/ruter-as/fido/internal/reports"
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

func TestScanCommand_CreatesErrorReports(t *testing.T) {
	server := newTestScanServer(t)
	defer server.Close()

	reportsDir := t.TempDir()
	mgr := reports.NewManager(reportsDir)
	ddClient := newTestDDClient(t, server.URL)

	cfg := &config.Config{
		Datadog: config.DatadogConfig{
			Services:     []string{"svc-a"},
			Site:         "test.datadoghq.com",
			OrgSubdomain: "myorg",
		},
		Scan: config.ScanConfig{Since: "24h"},
	}

	count, err := runScan(cfg, ddClient, mgr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 new issue, got %d", count)
	}

	if !mgr.Exists("issue-1") {
		t.Error("expected issue-1 report to exist")
	}
	content, _ := mgr.ReadError("issue-1")
	if !strings.Contains(content, "NullPointerException") {
		t.Error("expected error report to contain error title")
	}
	if !strings.Contains(content, "myorg") {
		t.Error("expected error report to contain Datadog links with org subdomain")
	}

	meta, err := mgr.ReadMetadata("issue-1")
	if err != nil {
		t.Fatalf("ReadMetadata: %v", err)
	}
	if meta.Service != "svc-a" {
		t.Errorf("expected service=svc-a, got %q", meta.Service)
	}
	if meta.Title != "NullPointerException" {
		t.Errorf("expected title=NullPointerException, got %q", meta.Title)
	}
	if !strings.Contains(meta.DatadogURL, "myorg") {
		t.Errorf("expected DatadogURL to contain myorg, got %q", meta.DatadogURL)
	}
	if !strings.Contains(meta.DatadogEventsURL, "myorg") && meta.DatadogEventsURL != "" {
		t.Errorf("expected DatadogEventsURL to contain myorg, got %q", meta.DatadogEventsURL)
	}
	if !strings.Contains(meta.DatadogTraceURL, "myorg") && meta.DatadogTraceURL != "" {
		t.Errorf("expected DatadogTraceURL to contain myorg, got %q", meta.DatadogTraceURL)
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

	cfg := &config.Config{
		Datadog: config.DatadogConfig{Services: []string{"svc-a"}},
		Scan:    config.ScanConfig{Since: "24h"},
	}

	count, err := runScan(cfg, ddClient, mgr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 new issues (existing skipped), got %d", count)
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
		Datadog: config.DatadogConfig{Services: []string{"svc-a"}},
		Scan:    config.ScanConfig{Since: "24h"},
	}

	_, err := runScan(cfg, ddClient, mgr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// meta.json should still have empty CIStatus
	meta, _ := mgr.ReadMetadata("issue-1")
	if meta.CIStatus != "" {
		t.Errorf("expected empty CIStatus, got %q", meta.CIStatus)
	}
}
