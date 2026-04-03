package datadog

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DataDog/datadog-api-client-go/v2/api/datadog"
)

// newTestClient creates a Client pointing at the given httptest server.
func newTestClient(t *testing.T, serverURL string) *Client {
	t.Helper()
	c, err := NewClient("test-token", "test.datadoghq.com", "myorg")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	c.OverrideServers(datadog.ServerConfigurations{
		{URL: serverURL, Description: "test"},
	})
	return c
}

func TestClient_SearchErrorIssues(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/error-tracking/issues/search" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("missing or incorrect Authorization header")
		}

		// Verify request body
		body, _ := io.ReadAll(r.Body)
		var reqBody struct {
			Data struct {
				Type       string `json:"type"`
				Attributes struct {
					Query string `json:"query"`
				} `json:"attributes"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &reqBody); err != nil {
			t.Errorf("failed to parse request body: %v", err)
		}
		if reqBody.Data.Type != "search_request" {
			t.Errorf("expected type search_request, got %s", reqBody.Data.Type)
		}
		if reqBody.Data.Attributes.Query != "service:svc-a" {
			t.Errorf("unexpected query: %s", reqBody.Data.Attributes.Query)
		}

		// Return SDK-compatible response with included issue details
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]interface{}{
			"data": []map[string]interface{}{
				{
					"id": "issue-1",
					"type": "issues_search_result",
					"attributes": map[string]interface{}{
						"total_count": 42,
					},
				},
			},
			"included": []map[string]interface{}{
				{
					"id":   "issue-1",
					"type": "issue",
					"attributes": map[string]interface{}{
						"error_type":    "NullPointerException",
						"error_message": "null pointer in handleRequest",
						"service":       "svc-a",
						"first_seen":    1711339200000,
						"last_seen":     1711342800000,
						"state":         "OPEN",
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	issues, err := client.SearchErrorIssues([]string{"svc-a"}, "24h")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	if issues[0].ID != "issue-1" {
		t.Errorf("expected issue-1, got %s", issues[0].ID)
	}
	if issues[0].Attributes.Title != "NullPointerException" {
		t.Errorf("expected NullPointerException, got %s", issues[0].Attributes.Title)
	}
	if issues[0].Attributes.Count != 42 {
		t.Errorf("expected count 42, got %d", issues[0].Attributes.Count)
	}
	if issues[0].Attributes.Service != "svc-a" {
		t.Errorf("expected service svc-a, got %s", issues[0].Attributes.Service)
	}
}

func TestClient_FetchIssueContext_ReturnsDeepLinks(t *testing.T) {
	c, err := NewClient("token", "datadoghq.eu", "myorg")
	if err != nil {
		t.Fatal(err)
	}
	c.OverrideServers(datadog.ServerConfigurations{
		{URL: "http://localhost:0"},
	})

	ctx, err := c.FetchIssueContext("payment-svc", "production", "2026-03-25T10:00:00Z", "2026-03-26T09:00:00Z")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ctx.EventsURL == "" {
		t.Error("expected EventsURL to be non-empty")
	}
	if ctx.TracesURL == "" {
		t.Error("expected TracesURL to be non-empty")
	}
}

func TestClient_FetchErrorTimeline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Simulate a timeseries compute response: one aggregate bucket whose
		// "c0" value is a JSON array of {time, value} points.
		resp := map[string]interface{}{
			"data": []map[string]interface{}{
				{
					"type": "spans_metrics",
					"attributes": map[string]interface{}{
						"computes": map[string]interface{}{
							// A timeseries is a JSON array of {time, value} objects.
							"c0": []map[string]interface{}{
								{"time": "2024-04-01T08:00:00+00:00", "value": float64(12)},
								{"time": "2024-04-01T09:00:00+00:00", "value": float64(8)},
							},
						},
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	buckets, err := client.FetchErrorTimeline("svc-a", "production", "24h")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(buckets) != 2 {
		t.Fatalf("expected 2 buckets, got %d", len(buckets))
	}
	if buckets[0].Count != 12 {
		t.Errorf("expected first bucket count 12, got %d", buckets[0].Count)
	}
	if buckets[0].Timestamp != "2024-04-01T08:00:00+00:00" {
		t.Errorf("expected first bucket timestamp 2024-04-01T08:00:00+00:00, got %s", buckets[0].Timestamp)
	}
}

func TestClient_SearchLogs(t *testing.T) {
	client, err := NewClient("test-token", "test.datadoghq.com", "myorg")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	logs, err := client.SearchLogs("trace_id:abc123", "1h")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(logs) != 0 {
		t.Fatalf("expected 0 logs (stubbed), got %d", len(logs))
	}
}
