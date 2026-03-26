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
	c, err := ddclient.NewClient("token", "test.datadoghq.com")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	c.OverrideServers(datadog.ServerConfigurations{
		{URL: serverURL, Description: "test"},
	})
	return c
}

func TestScanCommand_CreatesErrorReports(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	cfg := &config.Config{
		Datadog: config.DatadogConfig{
			Services: []string{"svc-a"},
			Site:     "test.datadoghq.com",
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
