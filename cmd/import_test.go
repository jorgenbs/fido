package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ruter-as/fido/internal/config"
	"github.com/ruter-as/fido/internal/reports"
)

func newTestImportServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "spans") {
			json.NewEncoder(w).Encode(map[string]interface{}{"data": []interface{}{}})
			return
		}
		resp := map[string]interface{}{
			"data": []map[string]interface{}{
				{
					"id":   "issue-abc",
					"type": "issues_search_result",
					"attributes": map[string]interface{}{
						"total_count": 42,
					},
				},
			},
			"included": []map[string]interface{}{
				{
					"id":   "issue-abc",
					"type": "issue",
					"attributes": map[string]interface{}{
						"error_type":    "TimeoutError",
						"error_message": "request timed out",
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

func TestImportIssue_Success(t *testing.T) {
	server := newTestImportServer(t)
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
		Repositories: map[string]config.RepoConfig{
			"svc-a": {Local: "/tmp/svc-a"},
		},
	}

	err := runImport("issue-abc", cfg, ddClient, mgr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !mgr.Exists("issue-abc") {
		t.Fatal("expected issue to be created")
	}

	meta, err := mgr.ReadMetadata("issue-abc")
	if err != nil {
		t.Fatalf("ReadMetadata: %v", err)
	}
	if meta.Title != "TimeoutError" {
		t.Errorf("title: got %q, want TimeoutError", meta.Title)
	}
	if meta.Service != "svc-a" {
		t.Errorf("service: got %q, want svc-a", meta.Service)
	}
}

func TestImportIssue_ServiceNotConfigured(t *testing.T) {
	server := newTestImportServer(t)
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
		Scan:         config.ScanConfig{Since: "24h"},
		Repositories: map[string]config.RepoConfig{},
	}

	err := runImport("issue-abc", cfg, ddClient, mgr)
	if err == nil {
		t.Fatal("expected error for unconfigured service")
	}
	if !strings.Contains(err.Error(), "not configured") {
		t.Errorf("expected 'not configured' error, got: %v", err)
	}
}

func TestImportIssue_AlreadyExists(t *testing.T) {
	server := newTestImportServer(t)
	defer server.Close()

	reportsDir := t.TempDir()
	mgr := reports.NewManager(reportsDir)
	mgr.WriteError("issue-abc", "existing")
	ddClient := newTestDDClient(t, server.URL)

	cfg := &config.Config{
		Datadog: config.DatadogConfig{
			Services:     []string{"svc-a"},
			Site:         "test.datadoghq.com",
			OrgSubdomain: "myorg",
		},
		Scan: config.ScanConfig{Since: "24h"},
		Repositories: map[string]config.RepoConfig{
			"svc-a": {Local: "/tmp/svc-a"},
		},
	}

	err := runImport("issue-abc", cfg, ddClient, mgr)
	if err == nil {
		t.Fatal("expected error for already-existing issue")
	}
	if !strings.Contains(err.Error(), "already imported") {
		t.Errorf("expected 'already imported' error, got: %v", err)
	}
}
