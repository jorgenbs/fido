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

	ctx, err := c.FetchIssueContext("test-issue-123", "payment-svc", "production", "2026-03-25T10:00:00Z", "2026-03-26T09:00:00Z")
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

func TestClient_FetchIssueContext_ExtractsTraceDetails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]interface{}{
			"data": []map[string]interface{}{
				{
					"type": "spans",
					"id":   "span-1",
					"attributes": map[string]interface{}{
						"trace_id": "abc123",
						"custom": map[string]interface{}{
							"error": map[string]interface{}{
								"name":    "DRTException",
								"message": "Spare API returned 422",
								"type":    "com.example.DRTException",
								"stack":   "DRTException\n  at Foo.bar(Foo.java:42)",
							},
							"http": map[string]interface{}{
								"method":      "POST",
								"url":         "https://spare.example.com/api/rides",
								"status_code": float64(422),
							},
							"response": map[string]interface{}{
								"body": "{\"error\":\"unprocessable\"}",
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
	ctx, err := client.FetchIssueContext("issue-xyz", "my-svc", "production", "2026-01-01T00:00:00Z", "2026-01-02T00:00:00Z")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	td := ctx.TraceDetails
	if td.ErrorName != "DRTException" {
		t.Errorf("ErrorName: got %q, want %q", td.ErrorName, "DRTException")
	}
	if td.ErrorMessage != "Spare API returned 422" {
		t.Errorf("ErrorMessage: got %q, want %q", td.ErrorMessage, "Spare API returned 422")
	}
	if td.ErrorType != "com.example.DRTException" {
		t.Errorf("ErrorType: got %q, want %q", td.ErrorType, "com.example.DRTException")
	}
	if td.HTTPMethod != "POST" {
		t.Errorf("HTTPMethod: got %q, want %q", td.HTTPMethod, "POST")
	}
	if td.HTTPURL != "https://spare.example.com/api/rides" {
		t.Errorf("HTTPURL: got %q, want %q", td.HTTPURL, "https://spare.example.com/api/rides")
	}
	if td.HTTPStatusCode != 422 {
		t.Errorf("HTTPStatusCode: got %d, want %d", td.HTTPStatusCode, 422)
	}
	if td.ResponseBody != "{\"error\":\"unprocessable\"}" {
		t.Errorf("ResponseBody: got %q, want %q", td.ResponseBody, "{\"error\":\"unprocessable\"}")
	}
	// Also verify StackTrace still works
	if ctx.StackTrace == "" {
		t.Error("expected StackTrace to be extracted")
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
