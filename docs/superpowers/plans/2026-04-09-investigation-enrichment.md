# Investigation Enrichment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Enrich the investigation agent prompt with richer Datadog context — trace payload details, error frequency buckets, version info, and co-occurring errors.

**Architecture:** All changes extend existing files. The Datadog client gets new extraction methods, the reports manager gets version fields, and the investigate command assembles enrichment sections into the prompt before invoking the agent.

**Tech Stack:** Go, Datadog API v2 SDK (`datadogV2`), existing `internal/datadog`, `internal/reports`, `cmd/investigate.go`

**Spec:** `docs/superpowers/specs/2026-04-09-investigation-enrichment-design.md`

---

### Task 1: Add TraceDetails to Datadog Client

**Files:**
- Modify: `internal/datadog/client.go:176-302` (IssueContext struct + FetchIssueContext method)
- Test: `internal/datadog/client_test.go`

- [ ] **Step 1: Write the failing test for TraceDetails extraction**

Add to `internal/datadog/client_test.go`:

```go
func TestClient_FetchIssueContext_ExtractsTraceDetails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]interface{}{
			"data": []map[string]interface{}{
				{
					"id":   "span-1",
					"type": "spans",
					"attributes": map[string]interface{}{
						"trace_id":        "abc123",
						"start_timestamp": "2026-03-26T16:13:52Z",
						"custom": map[string]interface{}{
							"error": map[string]interface{}{
								"stack":   "at SpareHttpClientConfig.kt:38",
								"name":    "ServiceNotAvailableError",
								"message": "Service is not available at the requested time",
								"type":    "DRTException",
							},
							"http": map[string]interface{}{
								"method":      "POST",
								"url":         "https://api.sparelabs.com/v1/rides",
								"status_code": float64(400),
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
	ctx, err := client.FetchIssueContext("test-issue", "drt-via", "", "2026-03-26T16:00:00Z", "2026-03-26T17:00:00Z")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ctx.TraceDetails.ErrorName != "ServiceNotAvailableError" {
		t.Errorf("ErrorName: got %q, want ServiceNotAvailableError", ctx.TraceDetails.ErrorName)
	}
	if ctx.TraceDetails.ErrorMessage != "Service is not available at the requested time" {
		t.Errorf("ErrorMessage: got %q", ctx.TraceDetails.ErrorMessage)
	}
	if ctx.TraceDetails.ErrorType != "DRTException" {
		t.Errorf("ErrorType: got %q, want DRTException", ctx.TraceDetails.ErrorType)
	}
	if ctx.TraceDetails.HTTPMethod != "POST" {
		t.Errorf("HTTPMethod: got %q, want POST", ctx.TraceDetails.HTTPMethod)
	}
	if ctx.TraceDetails.HTTPURL != "https://api.sparelabs.com/v1/rides" {
		t.Errorf("HTTPURL: got %q", ctx.TraceDetails.HTTPURL)
	}
	if ctx.TraceDetails.HTTPStatusCode != 400 {
		t.Errorf("HTTPStatusCode: got %d, want 400", ctx.TraceDetails.HTTPStatusCode)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/datadog/ -run TestClient_FetchIssueContext_ExtractsTraceDetails -v`
Expected: FAIL — `ctx.TraceDetails` does not exist

- [ ] **Step 3: Add TraceDetails struct and extraction logic**

In `internal/datadog/client.go`, add the struct after the existing `TraceRef` type (around line 188):

```go
// TraceDetails holds extracted fields from a span's custom attributes.
type TraceDetails struct {
	ErrorName      string
	ErrorMessage   string
	ErrorType      string
	HTTPMethod     string
	HTTPURL        string
	HTTPStatusCode int
	ResponseBody   string
}
```

Add `TraceDetails` field to `IssueContext`:

```go
type IssueContext struct {
	Traces       []TraceRef
	EventsURL    string
	TracesURL    string
	StackTrace   string
	TraceDetails TraceDetails
}
```

In `FetchIssueContext`, after the existing stack trace extraction block (around line 282-293), add trace details extraction from the same `custom` map. Replace the existing span loop body with:

```go
for i, span := range spans {
	attrs := span.GetAttributes()
	traceID := attrs.GetTraceId()
	if c.verbose {
		fmt.Fprintf(os.Stderr, "[datadog] span[%d]: traceID=%q\n", i, traceID)
	}
	if traceID == "" {
		continue
	}
	traceURL := fmt.Sprintf("https://%s.%s/apm/trace/%s", c.orgSubdomain, c.site, url.PathEscape(traceID))
	ctx.Traces = append(ctx.Traces, TraceRef{TraceID: traceID, URL: traceURL})

	custom := attrs.GetCustom()
	if custom == nil {
		continue
	}

	// Extract stack trace from first span that has one
	if ctx.StackTrace == "" {
		if errVal, ok := custom["error"]; ok {
			if errMap, ok := errVal.(map[string]interface{}); ok {
				if stack, ok := errMap["stack"].(string); ok && stack != "" {
					ctx.StackTrace = stack
				}
			}
		}
	}

	// Extract trace details from first span (only once)
	if ctx.TraceDetails.ErrorName == "" {
		if errVal, ok := custom["error"]; ok {
			if errMap, ok := errVal.(map[string]interface{}); ok {
				ctx.TraceDetails.ErrorName, _ = errMap["name"].(string)
				ctx.TraceDetails.ErrorMessage, _ = errMap["message"].(string)
				ctx.TraceDetails.ErrorType, _ = errMap["type"].(string)
			}
		}
		if httpVal, ok := custom["http"]; ok {
			if httpMap, ok := httpVal.(map[string]interface{}); ok {
				ctx.TraceDetails.HTTPMethod, _ = httpMap["method"].(string)
				ctx.TraceDetails.HTTPURL, _ = httpMap["url"].(string)
				if sc, ok := httpMap["status_code"].(float64); ok {
					ctx.TraceDetails.HTTPStatusCode = int(sc)
				}
			}
		}
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/datadog/ -run TestClient_FetchIssueContext -v`
Expected: ALL FetchIssueContext tests PASS

- [ ] **Step 5: Commit**

```bash
git add internal/datadog/client.go internal/datadog/client_test.go
git commit -m "feat: extract trace details (error name, HTTP info) from span payload"
```

---

### Task 2: Add FetchErrorFrequency to Datadog Client

**Files:**
- Modify: `internal/datadog/client.go`
- Test: `internal/datadog/client_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/datadog/client_test.go`:

```go
func TestClient_FetchErrorFrequency(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/spans/analytics/aggregate" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		// Simulate timeseries response with a single bucket containing timeseries points
		resp := map[string]interface{}{
			"data": []map[string]interface{}{
				{
					"id":   "bucket-1",
					"type": "aggregate_bucket",
					"attributes": map[string]interface{}{
						"computes": map[string]interface{}{
							"c0": []map[string]interface{}{
								{"time": "2026-03-26T00:00:00Z", "value": 1.0},
								{"time": "2026-03-27T00:00:00Z", "value": 0.0},
								{"time": "2026-04-07T00:00:00Z", "value": 5.0},
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
	freq, err := client.FetchErrorFrequency("test-issue", "drt-via", "", "2026-03-26T00:00:00Z", "2026-04-07T23:59:59Z")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if freq.TotalCount != 6 {
		t.Errorf("TotalCount: got %d, want 6", freq.TotalCount)
	}
	if len(freq.Buckets) != 2 {
		t.Errorf("Buckets: got %d, want 2 (zero-count buckets should be excluded)", len(freq.Buckets))
	}
	if freq.Buckets[0].Date != "2026-03-26" {
		t.Errorf("first bucket date: got %q, want 2026-03-26", freq.Buckets[0].Date)
	}
	if freq.TrendDirection == "" {
		t.Error("TrendDirection should be set")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/datadog/ -run TestClient_FetchErrorFrequency -v`
Expected: FAIL — `FetchErrorFrequency` does not exist

- [ ] **Step 3: Add FrequencyData types and FetchErrorFrequency method**

Add types to `internal/datadog/client.go` after the `IssueContext` / `TraceDetails` types:

```go
// FrequencyData holds error occurrence frequency over time.
type FrequencyData struct {
	Buckets        []FrequencyBucket
	OnsetTime      time.Time
	TotalCount     int64
	SpikeDates     []time.Time
	TrendDirection string // "increasing", "decreasing", "stable", "single_spike"
}

// FrequencyBucket is a single day's error count.
type FrequencyBucket struct {
	Date  string
	Count int64
}
```

Add the method:

```go
// FetchErrorFrequency returns daily error occurrence counts for the given issue
// using the Spans Aggregate API with timeseries compute.
func (c *Client) FetchErrorFrequency(issueID, service, env, firstSeen, lastSeen string) (FrequencyData, error) {
	from, err := time.Parse(time.RFC3339, firstSeen)
	if err != nil {
		from = time.Now().UTC().Add(-24 * time.Hour)
	}
	to, err := time.Parse(time.RFC3339, lastSeen)
	if err != nil {
		to = time.Now().UTC()
	}
	from = from.Add(-5 * time.Minute)
	to = to.Add(5 * time.Minute)

	query := fmt.Sprintf("service:%s status:error @issue.id:%s", service, issueID)
	if env != "" {
		query += " env:" + env
	}

	if c.verbose {
		fmt.Fprintf(os.Stderr, "[datadog] FetchErrorFrequency: query=%q from=%s to=%s\n",
			query, from.Format(time.RFC3339), to.Format(time.RFC3339))
	}

	computeType := datadogV2.SPANSCOMPUTETYPE_TIMESERIES
	body := datadogV2.SpansAggregateRequest{
		Data: &datadogV2.SpansAggregateData{},
	}
	body.Data.SetAttributes(datadogV2.SpansAggregateRequestAttributes{
		Compute: []datadogV2.SpansCompute{
			{
				Aggregation: datadogV2.SPANSAGGREGATIONFUNCTION_COUNT,
				Type:        &computeType,
				Interval:    datadog.PtrString("1d"),
			},
		},
		Filter: &datadogV2.SpansQueryFilter{
			Query: datadog.PtrString(query),
			From:  datadog.PtrString(from.Format(time.RFC3339)),
			To:    datadog.PtrString(to.Format(time.RFC3339)),
		},
	})
	body.Data.SetType(datadogV2.SPANSAGGREGATEREQUESTTYPE_AGGREGATE_REQUEST)

	resp, _, err := c.spansAPI.AggregateSpans(c.ctx(), body)
	if err != nil {
		return FrequencyData{}, fmt.Errorf("aggregating spans: %w", err)
	}

	return parseFrequencyResponse(resp), nil
}

// parseFrequencyResponse extracts daily buckets from the aggregate response.
func parseFrequencyResponse(resp datadogV2.SpansAggregateResponse) FrequencyData {
	var fd FrequencyData

	for _, bucket := range resp.GetData() {
		attrs := bucket.GetAttributes()
		computes := attrs.GetComputes()
		for _, val := range computes {
			if val.SpansAggregateBucketValueTimeseries == nil {
				continue
			}
			for _, pt := range val.SpansAggregateBucketValueTimeseries.Items {
				ts := pt.GetTime()
				count := int64(pt.GetValue())
				if count == 0 {
					continue
				}
				fd.TotalCount += count
				date := ts
				if t, err := time.Parse(time.RFC3339, ts); err == nil {
					date = t.Format("2006-01-02")
					if fd.OnsetTime.IsZero() || t.Before(fd.OnsetTime) {
						fd.OnsetTime = t
					}
				}
				fd.Buckets = append(fd.Buckets, FrequencyBucket{Date: date, Count: count})
			}
		}
	}

	fd.TrendDirection = computeTrend(fd.Buckets)
	fd.SpikeDates = computeSpikes(fd.Buckets)
	return fd
}

// computeTrend determines the trend direction from daily buckets.
func computeTrend(buckets []FrequencyBucket) string {
	if len(buckets) == 0 {
		return "stable"
	}
	if len(buckets) == 1 {
		return "single_spike"
	}
	first := buckets[0].Count
	last := buckets[len(buckets)-1].Count
	if last > first*2 {
		return "increasing"
	}
	if first > last*2 {
		return "decreasing"
	}
	return "stable"
}

// computeSpikes returns dates where the count exceeds 2× the average.
func computeSpikes(buckets []FrequencyBucket) []time.Time {
	if len(buckets) < 2 {
		return nil
	}
	var total int64
	for _, b := range buckets {
		total += b.Count
	}
	avg := float64(total) / float64(len(buckets))
	threshold := avg * 2

	var spikes []time.Time
	for _, b := range buckets {
		if float64(b.Count) > threshold {
			if t, err := time.Parse("2006-01-02", b.Date); err == nil {
				spikes = append(spikes, t)
			}
		}
	}
	return spikes
}
```

Note: Add `datadogV2 "github.com/DataDog/datadog-api-client-go/v2/api/datadogV2"` and `"github.com/DataDog/datadog-api-client-go/v2/api/datadog"` to the import block if not already present.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/datadog/ -run TestClient_FetchErrorFrequency -v`
Expected: PASS

- [ ] **Step 5: Run all datadog client tests**

Run: `go test ./internal/datadog/ -v`
Expected: ALL PASS

- [ ] **Step 6: Commit**

```bash
git add internal/datadog/client.go internal/datadog/client_test.go
git commit -m "feat: add FetchErrorFrequency for daily error occurrence timeseries"
```

---

### Task 3: Add Version Info to Search and Metadata

**Files:**
- Modify: `internal/datadog/client.go:27-42` (ErrorIssueAttributes struct)
- Modify: `internal/datadog/client.go:99-175` (SearchErrorIssues method)
- Modify: `internal/reports/manager.go:30-47` (MetaData struct)
- Modify: `cmd/scan.go:80-119` (runScan)
- Modify: `cmd/import.go:106-117` (meta creation in runImport)
- Test: `internal/datadog/client_test.go`
- Test: `cmd/scan_test.go`

- [ ] **Step 1: Write the failing test for version extraction**

Add to `internal/datadog/client_test.go`:

```go
func TestClient_SearchErrorIssues_ExtractsVersionInfo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
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
						"error_type":         "NullPointerException",
						"error_message":      "null pointer",
						"service":            "svc-a",
						"first_seen":         1711339200000,
						"last_seen":          1711342800000,
						"state":              "OPEN",
						"first_seen_version": "v2.4.1",
						"last_seen_version":  "v2.4.1",
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
	if issues[0].Attributes.FirstSeenVersion != "v2.4.1" {
		t.Errorf("FirstSeenVersion: got %q, want v2.4.1", issues[0].Attributes.FirstSeenVersion)
	}
	if issues[0].Attributes.LastSeenVersion != "v2.4.1" {
		t.Errorf("LastSeenVersion: got %q, want v2.4.1", issues[0].Attributes.LastSeenVersion)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/datadog/ -run TestClient_SearchErrorIssues_ExtractsVersionInfo -v`
Expected: FAIL — `FirstSeenVersion` field does not exist

- [ ] **Step 3: Add version fields to structs and extraction**

In `internal/datadog/client.go`, add to `ErrorIssueAttributes` (around line 37):

```go
type ErrorIssueAttributes struct {
	Title            string
	Message          string
	Service          string
	Env              string
	FirstSeen        string
	LastSeen         string
	Count            int64
	Status           string
	StackTrace       string
	FirstSeenVersion string
	LastSeenVersion  string
}
```

In `SearchErrorIssues`, inside the `if detail, ok := issueDetails[item.GetId()]; ok` block (around line 151), add version extraction after the existing field assignments:

```go
issue.Attributes.FirstSeenVersion = da.GetFirstSeenVersion()
issue.Attributes.LastSeenVersion = da.GetLastSeenVersion()
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/datadog/ -run TestClient_SearchErrorIssues -v`
Expected: ALL SearchErrorIssues tests PASS

- [ ] **Step 5: Add version fields to MetaData**

In `internal/reports/manager.go`, add to the `MetaData` struct (after `CodeFixable`):

```go
type MetaData struct {
	Title            string `json:"title"`
	Message          string `json:"message,omitempty"`
	Service          string `json:"service"`
	Env              string `json:"env"`
	FirstSeen        string `json:"first_seen"`
	LastSeen         string `json:"last_seen"`
	Count            int64  `json:"count"`
	DatadogURL       string `json:"datadog_url"`
	DatadogEventsURL string `json:"datadog_events_url"`
	DatadogTraceURL  string `json:"datadog_trace_url"`
	Ignored          bool   `json:"ignored"`
	CIStatus         string `json:"ci_status,omitempty"`
	CIURL            string `json:"ci_url,omitempty"`
	Confidence       string `json:"confidence,omitempty"`
	Complexity       string `json:"complexity,omitempty"`
	CodeFixable      string `json:"code_fixable,omitempty"`
	FirstSeenVersion string `json:"first_seen_version,omitempty"`
	LastSeenVersion  string `json:"last_seen_version,omitempty"`
}
```

- [ ] **Step 6: Persist version info in scan and import**

In `cmd/scan.go`, inside `runScan` (after `meta.Count = issue.Attributes.Count`, around line 107) and the same spot in `runScanWithResults` (around line 156), add:

```go
meta.FirstSeenVersion = issue.Attributes.FirstSeenVersion
meta.LastSeenVersion = issue.Attributes.LastSeenVersion
```

In `cmd/import.go`, inside `runImport` (after `meta.DatadogTraceURL`, around line 116), add:

```go
meta := &reports.MetaData{
	// ... existing fields ...
	FirstSeenVersion: found.Attributes.FirstSeenVersion,
	LastSeenVersion:  found.Attributes.LastSeenVersion,
}
```

- [ ] **Step 7: Run all tests**

Run: `go test ./... -v`
Expected: ALL PASS

- [ ] **Step 8: Commit**

```bash
git add internal/datadog/client.go internal/datadog/client_test.go internal/reports/manager.go cmd/scan.go cmd/import.go
git commit -m "feat: extract and persist version info from Datadog issue attributes"
```

---

### Task 4: Enrichment Formatting Functions

**Files:**
- Modify: `cmd/investigate.go`
- Test: `cmd/investigate_test.go`

This task adds the pure functions that format enrichment data into prompt sections. No Datadog calls — just formatting and conditional inclusion.

- [ ] **Step 1: Write the failing test for trace details formatting**

Add to `cmd/investigate_test.go`:

```go
func TestFormatTraceDetails_WithData(t *testing.T) {
	td := datadog.TraceDetails{
		ErrorName:      "ServiceNotAvailableError",
		ErrorMessage:   "Service is not available at the requested time",
		ErrorType:      "DRTException",
		HTTPMethod:     "POST",
		HTTPURL:        "https://api.sparelabs.com/v1/rides",
		HTTPStatusCode: 400,
	}
	result := formatTraceDetails(td)
	if result == "" {
		t.Fatal("expected non-empty result")
	}
	if !strings.Contains(result, "ServiceNotAvailableError") {
		t.Error("expected ErrorName in output")
	}
	if !strings.Contains(result, "POST") {
		t.Error("expected HTTP method in output")
	}
	if !strings.Contains(result, "400") {
		t.Error("expected status code in output")
	}
}

func TestFormatTraceDetails_Empty(t *testing.T) {
	result := formatTraceDetails(datadog.TraceDetails{})
	if result != "" {
		t.Errorf("expected empty string for empty trace details, got %q", result)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/ -run TestFormatTraceDetails -v`
Expected: FAIL — `formatTraceDetails` does not exist

- [ ] **Step 3: Implement formatTraceDetails**

Add to `cmd/investigate.go`:

```go
// formatTraceDetails renders the Trace Details prompt section.
// Returns empty string if no meaningful data exists beyond the stack trace.
func formatTraceDetails(td datadog.TraceDetails) string {
	if td.ErrorName == "" && td.HTTPMethod == "" {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## Trace Details\n\n")
	if td.ErrorName != "" || td.ErrorMessage != "" {
		sb.WriteString(fmt.Sprintf("**Error:** %s", td.ErrorName))
		if td.ErrorMessage != "" {
			sb.WriteString(fmt.Sprintf(" — %q", td.ErrorMessage))
		}
		if td.ErrorType != "" {
			sb.WriteString(fmt.Sprintf(" (type: %s)", td.ErrorType))
		}
		sb.WriteString("\n")
	}
	if td.HTTPMethod != "" {
		sb.WriteString(fmt.Sprintf("**HTTP:** %s %s", td.HTTPMethod, td.HTTPURL))
		if td.HTTPStatusCode > 0 {
			sb.WriteString(fmt.Sprintf(" → %d", td.HTTPStatusCode))
		}
		sb.WriteString("\n")
	}
	if td.ResponseBody != "" {
		sb.WriteString(fmt.Sprintf("**Response Body:** %s\n", td.ResponseBody))
	}
	return sb.String()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/ -run TestFormatTraceDetails -v`
Expected: PASS

- [ ] **Step 5: Write the failing test for frequency formatting**

Add `"time"` and `"github.com/ruter-as/fido/internal/datadog"` to the import block in `cmd/investigate_test.go`.

Add to `cmd/investigate_test.go`:

```go
func TestFormatFrequency_WithBuckets(t *testing.T) {
	fd := datadog.FrequencyData{
		Buckets: []datadog.FrequencyBucket{
			{Date: "2026-03-26", Count: 1},
			{Date: "2026-04-07", Count: 5},
		},
		OnsetTime:      time.Date(2026, 3, 26, 0, 0, 0, 0, time.UTC),
		TotalCount:     6,
		TrendDirection: "increasing",
	}
	version := "v2.4.1"
	result := formatFrequency(fd, version)
	if result == "" {
		t.Fatal("expected non-empty result")
	}
	if !strings.Contains(result, "2026-03-26") {
		t.Error("expected onset date in output")
	}
	if !strings.Contains(result, "6 occurrences") {
		t.Error("expected total count in output")
	}
	if !strings.Contains(result, "v2.4.1") {
		t.Error("expected version in output")
	}
	if !strings.Contains(result, "increasing") {
		t.Error("expected trend in output")
	}
}

func TestFormatFrequency_EmptyBuckets(t *testing.T) {
	fd := datadog.FrequencyData{}
	result := formatFrequency(fd, "")
	if result != "" {
		t.Errorf("expected empty string for no data, got %q", result)
	}
}

func TestFormatFrequency_OmitsEmptyVersion(t *testing.T) {
	fd := datadog.FrequencyData{
		Buckets:        []datadog.FrequencyBucket{{Date: "2026-03-26", Count: 1}},
		OnsetTime:      time.Date(2026, 3, 26, 0, 0, 0, 0, time.UTC),
		TotalCount:     1,
		TrendDirection: "single_spike",
	}
	result := formatFrequency(fd, "")
	if strings.Contains(result, "Version") {
		t.Error("should omit version line when empty")
	}
}
```

- [ ] **Step 6: Run test to verify it fails**

Run: `go test ./cmd/ -run TestFormatFrequency -v`
Expected: FAIL — `formatFrequency` does not exist

- [ ] **Step 7: Implement formatFrequency**

Add to `cmd/investigate.go`:

```go
// formatFrequency renders the Error Frequency prompt section.
// Returns empty string if no frequency data is available.
func formatFrequency(fd datadog.FrequencyData, version string) string {
	if len(fd.Buckets) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## Error Frequency\n\n")
	sb.WriteString("_This data shows when and how often the error occurs. Use it to narrow the time window for git investigation, but only if the pattern suggests a recent onset. A long-lived error with steady occurrences is unlikely to correlate with recent commits._\n\n")

	// Summary line
	onset := fd.OnsetTime.Format("2006-01-02")
	daySpan := int(time.Since(fd.OnsetTime).Hours()/24) + 1
	sb.WriteString(fmt.Sprintf("**Summary:** First appeared %s, %d occurrences over %d days, trend: %s\n",
		onset, fd.TotalCount, daySpan, fd.TrendDirection))

	if version != "" {
		sb.WriteString(fmt.Sprintf("**Version:** First seen in %s\n", version))
	}

	// Compact bucket line
	sb.WriteString("**Buckets:**\n")
	parts := make([]string, len(fd.Buckets))
	for i, b := range fd.Buckets {
		parts[i] = fmt.Sprintf("%s: %d", b.Date, b.Count)
	}
	sb.WriteString(strings.Join(parts, " | ") + "\n")

	return sb.String()
}
```

- [ ] **Step 8: Run test to verify it passes**

Run: `go test ./cmd/ -run TestFormatFrequency -v`
Expected: PASS

- [ ] **Step 9: Write the failing test for related errors formatting**

Add to `cmd/investigate_test.go`:

```go
func TestFormatRelatedErrors_WithMatches(t *testing.T) {
	current := &reports.MetaData{
		Service:  "drt-via",
		FirstSeen: "2026-03-26T16:00:00Z",
	}
	issues := []reports.IssueSummary{
		{
			ID:    "other-issue-1",
			Stage: reports.StageInvestigated,
			Meta: &reports.MetaData{
				Title:    "TimeoutException",
				Message:  "Connection timed out",
				Service:  "drt-via",
				FirstSeen: "2026-03-26T15:30:00Z",
				Count:    5,
			},
		},
		{
			ID:    "other-issue-2",
			Stage: reports.StageScanned,
			Meta: &reports.MetaData{
				Title:    "OtherError",
				Service:  "different-svc",
				FirstSeen: "2026-03-26T16:00:00Z",
				Count:    1,
			},
		},
		{
			ID:    "other-issue-3",
			Stage: reports.StageScanned,
			Meta: &reports.MetaData{
				Title:    "OldError",
				Service:  "drt-via",
				FirstSeen: "2025-01-01T00:00:00Z",
				Count:    100,
			},
		},
	}
	result := formatRelatedErrors("current-issue", current, issues)
	if !strings.Contains(result, "TimeoutException") {
		t.Error("expected matching issue in output")
	}
	if strings.Contains(result, "OtherError") {
		t.Error("should not include issues from different services")
	}
	if strings.Contains(result, "OldError") {
		t.Error("should not include issues outside ±1h window")
	}
}

func TestFormatRelatedErrors_NoMatches(t *testing.T) {
	current := &reports.MetaData{
		Service:  "drt-via",
		FirstSeen: "2026-03-26T16:00:00Z",
	}
	result := formatRelatedErrors("current-issue", current, nil)
	if result != "" {
		t.Errorf("expected empty string when no issues, got %q", result)
	}
}
```

- [ ] **Step 10: Run test to verify it fails**

Run: `go test ./cmd/ -run TestFormatRelatedErrors -v`
Expected: FAIL — `formatRelatedErrors` does not exist

- [ ] **Step 11: Implement formatRelatedErrors**

Add to `cmd/investigate.go`:

```go
// formatRelatedErrors renders the Potentially Related Errors section.
// Finds issues in the same service with first_seen within ±1h of the current issue.
// Returns empty string if no co-occurring issues found.
func formatRelatedErrors(currentIssueID string, current *reports.MetaData, allIssues []reports.IssueSummary) string {
	if current == nil {
		return ""
	}
	currentTime, err := time.Parse(time.RFC3339, current.FirstSeen)
	if err != nil {
		return ""
	}

	var matches []string
	for _, issue := range allIssues {
		if issue.ID == currentIssueID {
			continue
		}
		if issue.Meta == nil || issue.Meta.Service != current.Service {
			continue
		}
		issueTime, err := time.Parse(time.RFC3339, issue.Meta.FirstSeen)
		if err != nil {
			continue
		}
		diff := currentTime.Sub(issueTime)
		if diff < 0 {
			diff = -diff
		}
		if diff > time.Hour {
			continue
		}
		matches = append(matches, fmt.Sprintf("- %s: %s (%s) — first seen %s, %d occurrences",
			issue.Meta.Title, issue.Meta.Message, issue.Meta.Service,
			issueTime.Format("2006-01-02 15:04"), issue.Meta.Count))
	}

	if len(matches) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Potentially Related Errors\n\n")
	sb.WriteString("_These errors were tracked in the same service with overlapping timelines. They may share a root cause, or they may be entirely unrelated. Only pursue connections if the error types or stack traces suggest a shared code path._\n\n")
	for _, m := range matches {
		sb.WriteString(m + "\n")
	}
	return sb.String()
}
```

- [ ] **Step 12: Run test to verify it passes**

Run: `go test ./cmd/ -run TestFormatRelatedErrors -v`
Expected: PASS

- [ ] **Step 13: Run all tests**

Run: `go test ./... -v`
Expected: ALL PASS

- [ ] **Step 14: Commit**

```bash
git add cmd/investigate.go cmd/investigate_test.go
git commit -m "feat: add enrichment formatting functions for investigation prompt"
```

---

### Task 5: Wire Enrichment Into the Investigation Prompt

**Files:**
- Modify: `cmd/investigate.go:56-83` (prompt template), `cmd/investigate.go:85-172` (runInvestigate)
- Test: `cmd/investigate_test.go`

- [ ] **Step 1: Write the failing test for enriched prompt**

Add to `cmd/investigate_test.go`:

```go
func TestInvestigate_IncludesEnrichmentSections(t *testing.T) {
	reportsDir := t.TempDir()
	repoDir := t.TempDir()
	mgr := reports.NewManager(reportsDir)

	// Create the issue under investigation
	mgr.WriteError("issue-1", "# Error\n**Service:** svc-a\nDRTException in handler")
	mgr.WriteMetadata("issue-1", &reports.MetaData{
		Service:          "svc-a",
		FirstSeen:        "2026-03-26T16:00:00Z",
		LastSeen:         "2026-03-26T17:00:00Z",
		FirstSeenVersion: "v2.4.1",
		LastSeenVersion:  "v2.4.1",
	})

	// Create a co-occurring issue in the same service
	mgr.WriteError("issue-2", "# Error\n**Service:** svc-a\nTimeoutException")
	mgr.WriteMetadata("issue-2", &reports.MetaData{
		Title:    "TimeoutException",
		Message:  "Connection timed out",
		Service:  "svc-a",
		FirstSeen: "2026-03-26T15:30:00Z",
		LastSeen:  "2026-03-26T17:00:00Z",
		Count:    3,
	})

	// Use "cat" as agent so the prompt becomes the investigation output
	cfg := &config.Config{
		Repositories: map[string]config.RepoConfig{"svc-a": {Local: repoDir}},
		Agent:        config.AgentConfig{Investigate: "cat"},
	}

	err := runInvestigate("issue-1", "svc-a", cfg, mgr, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	inv, _ := mgr.ReadInvestigation("issue-1")

	// Should include related errors section (issue-2 is same service, within ±1h)
	if !strings.Contains(inv, "TimeoutException") {
		t.Error("expected related error TimeoutException in investigation prompt")
	}
	if !strings.Contains(inv, "Potentially Related Errors") {
		t.Error("expected Related Errors section header")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/ -run TestInvestigate_IncludesEnrichmentSections -v`
Expected: FAIL — investigation output does not contain enrichment sections

- [ ] **Step 3: Update the prompt template**

In `cmd/investigate.go`, update `investigatePromptTemplate` to include a slot for enrichment and add the guidance:

```go
const investigatePromptTemplate = `You are investigating a production error. Analyze the error below, look through the codebase, and produce a root cause analysis.

## Error Report

%s
%s
## Instructions

0. Ensure you are on the latest on main branch
1. Analyze the error and stack trace
2. Find the relevant code in the repository
3. Identify the root cause
4. List all affected files and code paths
5. Suggest a fix approach
6. Estimate confidence and complexity
7. Determine whether this is a code-fixable defect

If the error frequency shows a sudden onset correlating with a version change, use git log and git blame to identify the introducing commit. The trace details may contain the exact error identifiers needed for the fix.

## Output Format

Write your analysis as markdown with these sections:
- **Root Cause**: What is causing this error
- **Affected Files**: List of files involved
- **Suggested Fix**: How to fix this

## Confidence: High/Medium/Low
## Complexity: Simple/Moderate/Complex
## Code Fixable: Yes/No (is this a code defect that can be fixed with a code change?)
`
```

- [ ] **Step 4: Update runInvestigate to build enrichment context**

In `cmd/investigate.go`, in the `runInvestigate` function, after the existing context-fetch block (after line 141) and before the prompt assembly (line 149), add enrichment building:

```go
	// Build enrichment sections
	var enrichment strings.Builder

	// Trace details (from enriched FetchIssueContext)
	if ddClient != nil {
		meta, metaErr := mgr.ReadMetadata(issueID)
		if metaErr == nil {
			if issueCtx, err := ddClient.FetchIssueContext(issueID, meta.Service, meta.Env, meta.FirstSeen, meta.LastSeen); err == nil {
				if td := formatTraceDetails(issueCtx.TraceDetails); td != "" {
					enrichment.WriteString(td)
					enrichment.WriteString("\n")
				}
			}
		}
	}

	// Error frequency
	if ddClient != nil {
		meta, metaErr := mgr.ReadMetadata(issueID)
		if metaErr == nil {
			freq, err := ddClient.FetchErrorFrequency(issueID, meta.Service, meta.Env, meta.FirstSeen, meta.LastSeen)
			if err == nil {
				// Apply version filtering: only include if first == last and non-empty
				version := ""
				if meta.FirstSeenVersion != "" && meta.FirstSeenVersion == meta.LastSeenVersion {
					version = meta.FirstSeenVersion
				}
				if fd := formatFrequency(freq, version); fd != "" {
					enrichment.WriteString(fd)
					enrichment.WriteString("\n")
				}
			}
		}
	}

	// Co-occurring errors (local data only)
	meta, metaErr := mgr.ReadMetadata(issueID)
	if metaErr == nil {
		allIssues, listErr := mgr.ListIssues(false)
		if listErr == nil {
			if re := formatRelatedErrors(issueID, meta, allIssues); re != "" {
				enrichment.WriteString(re)
				enrichment.WriteString("\n")
			}
		}
	}

	prompt := fmt.Sprintf(investigatePromptTemplate, errorContent, enrichment.String())
```

Note: The existing `prompt := fmt.Sprintf(investigatePromptTemplate, errorContent)` line (around line 149) is replaced by the block above. Also note: the `FetchIssueContext` call already exists earlier for traces/stacktrace enrichment of `error.md`. The enrichment block should reuse that result rather than calling it again. Refactor to store the `issueCtx` result and reuse it:

Actually, looking more carefully — the existing code calls `FetchIssueContext` to fill in `<!-- TRACES_PENDING -->` and `<!-- STACK_TRACE_PENDING -->` markers in `error.md`. The enrichment `TraceDetails` comes from the same call. So refactor to save the result:

In the existing `needsContext` block (around lines 92-141), save the `issueCtx` to a variable at function scope:

```go
var issueCtx *datadog.IssueContext
```

Then in the existing fetch block, store it:

```go
if ctx, err := ddClient.FetchIssueContext(issueID, meta.Service, meta.Env, meta.FirstSeen, meta.LastSeen); err == nil {
    issueCtx = &ctx
    // ... existing trace/stacktrace handling ...
}
```

And the enrichment block uses the saved result:

```go
// Trace details (reuse existing FetchIssueContext result if available)
if issueCtx == nil && ddClient != nil {
    if meta, err := mgr.ReadMetadata(issueID); err == nil {
        if ctx, err := ddClient.FetchIssueContext(issueID, meta.Service, meta.Env, meta.FirstSeen, meta.LastSeen); err == nil {
            issueCtx = &ctx
        }
    }
}
if issueCtx != nil {
    if td := formatTraceDetails(issueCtx.TraceDetails); td != "" {
        enrichment.WriteString(td)
        enrichment.WriteString("\n")
    }
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./cmd/ -run TestInvestigate_IncludesEnrichmentSections -v`
Expected: PASS

- [ ] **Step 6: Run all tests**

Run: `go test ./... -v`
Expected: ALL PASS

- [ ] **Step 7: Commit**

```bash
git add cmd/investigate.go cmd/investigate_test.go
git commit -m "feat: wire enrichment sections into investigation prompt"
```

---

### Task 6: Integration Verification Against Live Server

**Files:** None modified — verification only

Per CLAUDE.md: "After implementing or changing any API endpoint, verify it against the running server."

The investigation enrichment doesn't change API endpoints, but we should verify the end-to-end flow works with a real issue.

- [ ] **Step 1: Build and verify compilation**

```bash
make build
```

Expected: Clean build, no errors

- [ ] **Step 2: Run the full test suite**

```bash
go test ./...
```

Expected: ALL PASS

- [ ] **Step 3: Run investigation on a real issue with verbose logging**

Use one of the debug story issues to verify enrichment shows up:

```bash
./fido investigate c97d40f6-292e-11f1-a746-da7ad0900005 -v
```

Check the verbose output for:
- `[investigate]` logs showing frequency fetch
- `[datadog]` logs showing AggregateSpans call
- The resulting `investigation.md` should contain richer context than before

- [ ] **Step 4: Verify frontend renders correctly**

```bash
cd web && npm run dev
# In another terminal:
cd web && node verify.mjs
```

Expected: No React errors (enrichment changes don't affect frontend, but verify nothing broke)
