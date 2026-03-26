package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ruter-as/fido/internal/config"
	"github.com/ruter-as/fido/internal/datadog"
	"github.com/ruter-as/fido/internal/reports"
)

func TestScanCommand_CreatesErrorReports(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "error-tracking"):
			resp := datadog.SearchIssuesResponse{
				Data: []datadog.ErrorIssue{
					{
						ID: "issue-1",
						Attributes: datadog.ErrorIssueAttributes{
							Title:   "NullPointerException",
							Message: "null in handler",
							Service: "svc-a",
							Env:     "prod",
							Count:   10,
							Status:  "open",
						},
					},
				},
			}
			json.NewEncoder(w).Encode(resp)
		case strings.Contains(r.URL.Path, "logs"):
			resp := datadog.SearchLogsResponse{Data: []datadog.LogEntry{}}
			json.NewEncoder(w).Encode(resp)
		}
	}))
	defer server.Close()

	reportsDir := t.TempDir()
	mgr := reports.NewManager(reportsDir)
	ddClient := datadog.NewClient("token", server.URL)

	cfg := &config.Config{
		Datadog: config.DatadogConfig{
			Services: []string{"svc-a"},
			Site:     server.URL,
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
}

func TestScanCommand_SkipsExistingIssues(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := datadog.SearchIssuesResponse{
			Data: []datadog.ErrorIssue{
				{ID: "issue-1", Attributes: datadog.ErrorIssueAttributes{Title: "Err", Service: "svc-a"}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	reportsDir := t.TempDir()
	mgr := reports.NewManager(reportsDir)
	ddClient := datadog.NewClient("token", server.URL)

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
