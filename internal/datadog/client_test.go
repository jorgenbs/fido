package datadog

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DataDog/datadog-api-client-go/v2/api/datadog"
)

// newTestClient creates a Client pointing at the given httptest server.
func newTestClient(t *testing.T, serverURL string) *Client {
	t.Helper()
	c, err := NewClient(ClientConfig{Token: "test-token", Site: "test.datadoghq.com", OrgSubdomain: "myorg"})
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
	c, err := NewClient(ClientConfig{Token: "token", Site: "datadoghq.eu", OrgSubdomain: "myorg"})
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

func TestClient_FetchErrorFrequency(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/spans/analytics/aggregate" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}

		w.Header().Set("Content-Type", "application/json")
		resp := `{"data": [{"id":"bucket-1","type":"aggregate_bucket","attributes":{"computes":{"c0":[{"time":"2026-03-26T00:00:00Z","value":1.0},{"time":"2026-03-27T00:00:00Z","value":0.0},{"time":"2026-04-07T00:00:00Z","value":5.0}]}}}]}`
		w.Write([]byte(resp))
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	freq, err := client.FetchErrorFrequency("issue-123", "my-svc", "production", "2026-03-25T10:00:00Z", "2026-04-08T09:00:00Z")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if freq.TotalCount != 6 {
		t.Errorf("expected TotalCount=6, got %d", freq.TotalCount)
	}
	if len(freq.Buckets) != 2 {
		t.Errorf("expected 2 buckets (zero-count excluded), got %d", len(freq.Buckets))
	}
	if len(freq.Buckets) > 0 && freq.Buckets[0].Date != "2026-03-26" {
		t.Errorf("expected first bucket date 2026-03-26, got %s", freq.Buckets[0].Date)
	}
	if freq.TrendDirection == "" {
		t.Error("expected TrendDirection to be non-empty")
	}
}

func TestComputeTrend(t *testing.T) {
	tests := []struct {
		name    string
		buckets []FrequencyBucket
		want    string
	}{
		{"empty", nil, "stable"},
		{"single", []FrequencyBucket{{Date: "2026-01-01", Count: 5}}, "single_spike"},
		{"increasing", []FrequencyBucket{{Date: "2026-01-01", Count: 1}, {Date: "2026-01-02", Count: 10}}, "increasing"},
		{"decreasing", []FrequencyBucket{{Date: "2026-01-01", Count: 10}, {Date: "2026-01-02", Count: 1}}, "decreasing"},
		{"stable", []FrequencyBucket{{Date: "2026-01-01", Count: 5}, {Date: "2026-01-02", Count: 5}}, "stable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeTrend(tt.buckets)
			if got != tt.want {
				t.Errorf("computeTrend() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestComputeSpikes(t *testing.T) {
	buckets := []FrequencyBucket{
		{Date: "2026-01-01", Count: 1},
		{Date: "2026-01-02", Count: 1},
		{Date: "2026-01-03", Count: 10}, // spike: 10 > 2*avg(4)
	}
	spikes := computeSpikes(buckets)
	if len(spikes) != 1 {
		t.Fatalf("expected 1 spike, got %d", len(spikes))
	}
	if spikes[0].Format("2006-01-02") != "2026-01-03" {
		t.Errorf("expected spike on 2026-01-03, got %s", spikes[0].Format("2006-01-02"))
	}
}

func TestClient_SearchErrorIssues_ExtractsVersionInfo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]interface{}{
			"data": []map[string]interface{}{
				{
					"id":   "issue-v",
					"type": "issues_search_result",
					"attributes": map[string]interface{}{
						"total_count": 5,
					},
				},
			},
			"included": []map[string]interface{}{
				{
					"id":   "issue-v",
					"type": "issue",
					"attributes": map[string]interface{}{
						"error_type":          "VersionError",
						"error_message":       "version mismatch",
						"service":             "svc-b",
						"first_seen":          1711339200000,
						"last_seen":           1711342800000,
						"state":               "OPEN",
						"first_seen_version":  "v2.4.1",
						"last_seen_version":   "v2.4.1",
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	issues, err := client.SearchErrorIssues([]string{"svc-b"}, "24h")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	if issues[0].Attributes.FirstSeenVersion != "v2.4.1" {
		t.Errorf("FirstSeenVersion: got %q, want %q", issues[0].Attributes.FirstSeenVersion, "v2.4.1")
	}
	if issues[0].Attributes.LastSeenVersion != "v2.4.1" {
		t.Errorf("LastSeenVersion: got %q, want %q", issues[0].Attributes.LastSeenVersion, "v2.4.1")
	}
}

func TestClient_ResolveIssue(t *testing.T) {
	var receivedMethod, receivedPath string
	var receivedBody []byte

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		receivedPath = r.URL.Path
		receivedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		fmt.Fprintf(w, `{"data":{"id":"abc123","type":"error_tracking_issue","attributes":{"state":"RESOLVED"}}}`)
	}))
	defer ts.Close()

	client, err := NewClient(ClientConfig{Token: "test-token", Site: "datadoghq.com", OrgSubdomain: "myorg"})
	if err != nil {
		t.Fatal(err)
	}
	client.OverrideServers(datadog.ServerConfigurations{
		{URL: ts.URL, Description: "test"},
	})

	if err := client.ResolveIssue("abc123"); err != nil {
		t.Fatalf("ResolveIssue failed: %v", err)
	}

	if receivedMethod != "PUT" {
		t.Errorf("expected PUT, got %s", receivedMethod)
	}
	if !strings.Contains(receivedPath, "/abc123/state") {
		t.Errorf("expected path containing /abc123/state, got %s", receivedPath)
	}
	if !strings.Contains(string(receivedBody), `"RESOLVED"`) {
		t.Errorf("expected body to contain RESOLVED, got %s", receivedBody)
	}
}

func TestClient_GetIssueStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("expected GET, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"data":{"id":"abc123","type":"error_tracking_issue","attributes":{"state":"OPEN","error_type":"RuntimeError","service":"my-svc"}}}`)
	}))
	defer ts.Close()

	client, err := NewClient(ClientConfig{Token: "test-token", Site: "datadoghq.com", OrgSubdomain: "myorg"})
	if err != nil {
		t.Fatal(err)
	}
	client.OverrideServers(datadog.ServerConfigurations{
		{URL: ts.URL, Description: "test"},
	})

	status, err := client.GetIssueStatus("abc123")
	if err != nil {
		t.Fatalf("GetIssueStatus failed: %v", err)
	}
	if status != "OPEN" {
		t.Errorf("expected OPEN, got %s", status)
	}
}

func TestNewClient_APIKeyAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("API key auth should not send Bearer token")
		}
		if r.Header.Get("DD-API-KEY") == "" {
			t.Error("expected DD-API-KEY header")
		}
		if r.Header.Get("DD-APPLICATION-KEY") == "" {
			t.Error("expected DD-APPLICATION-KEY header")
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"data":{"id":"abc123","type":"error_tracking_issue","attributes":{"state":"OPEN"}}}`)
	}))
	defer server.Close()

	c, err := NewClient(ClientConfig{APIKey: "test-api-key", AppKey: "test-app-key", Site: "test.datadoghq.com", OrgSubdomain: "myorg"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	c.OverrideServers(datadog.ServerConfigurations{
		{URL: server.URL, Description: "test"},
	})

	status, err := c.GetIssueStatus("abc123")
	if err != nil {
		t.Fatalf("GetIssueStatus: %v", err)
	}
	if status != "OPEN" {
		t.Errorf("expected OPEN, got %s", status)
	}
}

func TestNewClient_ValidationErrors(t *testing.T) {
	tests := []struct {
		name string
		cc   ClientConfig
		want string
	}{
		{"no auth", ClientConfig{Site: "test.com"}, "datadog auth is required"},
		{"no site", ClientConfig{Token: "x"}, "site is required"},
		{"api_key without app_key", ClientConfig{APIKey: "x", Site: "test.com"}, "app_key is required"},
		{"app_key without api_key", ClientConfig{AppKey: "x", Site: "test.com"}, "api_key is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewClient(tt.cc)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("expected error containing %q, got %q", tt.want, err.Error())
			}
		})
	}
}

func TestNewClient_PATPreferredOverAPIKey(t *testing.T) {
	c, err := NewClient(ClientConfig{Token: "pat", APIKey: "ak", AppKey: "appk", Site: "test.com"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if c.authMode != AuthPAT {
		t.Error("expected PAT auth mode when token is set")
	}
}

func TestClient_SearchLogs(t *testing.T) {
	client, err := NewClient(ClientConfig{Token: "test-token", Site: "test.datadoghq.com", OrgSubdomain: "myorg"})
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
