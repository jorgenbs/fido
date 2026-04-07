package datadog

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/DataDog/datadog-api-client-go/v2/api/datadog"
	"github.com/DataDog/datadog-api-client-go/v2/api/datadogV2"
)

type Client struct {
	token        string
	site         string
	orgSubdomain string
	verbose      bool
	api          *datadogV2.ErrorTrackingApi
	spansAPI     *datadogV2.SpansApi
	cfg          *datadog.Configuration
}

// ErrorIssue is a simplified view of a Datadog error tracking issue.
type ErrorIssue struct {
	ID         string
	Attributes ErrorIssueAttributes
}

type ErrorIssueAttributes struct {
	Title      string
	Message    string
	Service    string
	Env        string
	FirstSeen  string
	LastSeen   string
	Count      int64
	Status     string
	StackTrace string
}

// LogEntry and LogAttributes are kept for the scan template.
type LogEntry struct {
	Attributes LogAttributes
}

type LogAttributes struct {
	Message   string
	Timestamp string
	Service   string
	Status    string
}

func NewClient(token, site, orgSubdomain string) (*Client, error) {
	if token == "" {
		return nil, fmt.Errorf("datadog token is required")
	}
	if site == "" {
		return nil, fmt.Errorf("datadog site is required")
	}

	cfg := datadog.NewConfiguration()

	c := &Client{
		token:        token,
		site:         site,
		orgSubdomain: orgSubdomain,
		cfg:          cfg,
	}

	// Inject a custom transport that adds Bearer auth for PAT
	cfg.HTTPClient = &http.Client{
		Timeout:   30 * time.Second,
		Transport: &bearerTransport{token: token, base: http.DefaultTransport},
	}

	apiClient := datadog.NewAPIClient(cfg)
	c.api = datadogV2.NewErrorTrackingApi(apiClient)
	c.spansAPI = datadogV2.NewSpansApi(apiClient)

	return c, nil
}

func (c *Client) SetVerbose(v bool) {
	c.verbose = v
	c.cfg.Debug = v
}

func (c *Client) ctx() context.Context {
	return context.WithValue(
		context.Background(),
		datadog.ContextServerVariables,
		map[string]string{"site": c.site},
	)
}

func (c *Client) SearchErrorIssues(services []string, since string) ([]ErrorIssue, error) {
	dur, err := time.ParseDuration(since)
	if err != nil {
		return nil, fmt.Errorf("invalid since duration %q: %w", since, err)
	}

	now := time.Now()
	from := now.Add(-dur)

	queryParts := []string{}
	for _, svc := range services {
		queryParts = append(queryParts, "service:"+svc)
	}
	queryStr := strings.Join(queryParts, " OR ")

	attrs := datadogV2.NewIssuesSearchRequestDataAttributes(from.UnixMilli(), queryStr, now.UnixMilli())
	attrs.SetTrack(datadogV2.ISSUESSEARCHREQUESTDATAATTRIBUTESTRACK_TRACE)

	reqBody := datadogV2.IssuesSearchRequest{
		Data: datadogV2.IssuesSearchRequestData{
			Attributes: *attrs,
			Type:       datadogV2.ISSUESSEARCHREQUESTDATATYPE_SEARCH_REQUEST,
		},
	}

	// Include full issue details in the response
	opts := datadogV2.NewSearchIssuesOptionalParameters().
		WithInclude([]datadogV2.SearchIssuesIncludeQueryParameterItem{
			datadogV2.SEARCHISSUESINCLUDEQUERYPARAMETERITEM_ISSUE,
		})

	resp, _, err := c.api.SearchIssues(c.ctx(), reqBody, *opts)
	if err != nil {
		return nil, fmt.Errorf("searching error issues: %w", err)
	}

	// Build a map of included issues by ID for quick lookup
	issueDetails := map[string]*datadogV2.Issue{}
	for _, inc := range resp.GetIncluded() {
		if inc.Issue != nil {
			issueDetails[inc.Issue.GetId()] = inc.Issue
		}
	}

	var issues []ErrorIssue
	for _, item := range resp.GetData() {
		searchAttrs := item.GetAttributes()
		issue := ErrorIssue{ID: item.GetId()}

		// Try to get full details from included issues
		if detail, ok := issueDetails[item.GetId()]; ok {
			da := detail.GetAttributes()
			issue.Attributes = ErrorIssueAttributes{
				Title:   da.GetErrorType(),
				Message: da.GetErrorMessage(),
				Service: da.GetService(),
				Count:   searchAttrs.GetTotalCount(),
				Status:  string(da.GetState()),
			}
			if fs := da.GetFirstSeen(); fs != 0 {
				issue.Attributes.FirstSeen = time.UnixMilli(fs).UTC().Format(time.RFC3339)
			}
			if ls := da.GetLastSeen(); ls != 0 {
				issue.Attributes.LastSeen = time.UnixMilli(ls).UTC().Format(time.RFC3339)
			}
		} else {
			// Fallback: minimal data from search result
			issue.Attributes = ErrorIssueAttributes{
				Count: searchAttrs.GetTotalCount(),
			}
		}

		issues = append(issues, issue)
	}

	return issues, nil
}

// IssueContext holds deep-link URLs, trace references, and a stack trace for a Datadog issue.
type IssueContext struct {
	Traces     []TraceRef
	EventsURL  string
	TracesURL  string
	StackTrace string
}

// TraceRef is a reference to a single Datadog trace.
type TraceRef struct {
	TraceID string
	URL     string
}

// FetchIssueContext builds deep-link URLs and attempts to fetch sample traces
// for the given issue. Uses @issue.id to filter spans to the correct error tracking issue.
func (c *Client) FetchIssueContext(issueID, service, env, firstSeen, lastSeen string) (IssueContext, error) {
	from, err := time.Parse(time.RFC3339, firstSeen)
	if err != nil || from.IsZero() {
		if c.verbose {
			fmt.Fprintf(os.Stderr, "[datadog] FetchIssueContext: firstSeen parse error (%v), defaulting to 24h ago\n", err)
		}
		from = time.Now().UTC().Add(-24 * time.Hour)
	}
	to, err2 := time.Parse(time.RFC3339, lastSeen)
	if err2 != nil || to.IsZero() {
		if c.verbose {
			fmt.Fprintf(os.Stderr, "[datadog] FetchIssueContext: lastSeen parse error (%v), defaulting to now\n", err2)
		}
		to = time.Now().UTC()
	}
	from = from.Add(-5 * time.Minute)
	to = to.Add(5 * time.Minute)

	eventsURL := fmt.Sprintf(
		"https://%s.%s/event/explorer?query=%s&from=%d&to=%d",
		c.orgSubdomain, c.site,
		url.QueryEscape(fmt.Sprintf("service:%s env:%s", service, env)),
		from.UnixMilli(), to.UnixMilli(),
	)
	tracesURL := fmt.Sprintf(
		"https://%s.%s/apm/traces?query=%s&start=%d&end=%d",
		c.orgSubdomain, c.site,
		url.QueryEscape(fmt.Sprintf("service:%s env:%s", service, env)),
		from.UnixMilli(), to.UnixMilli(),
	)

	ctx := IssueContext{
		EventsURL: eventsURL,
		TracesURL: tracesURL,
	}

	// Use @issue.id to filter spans to this specific error tracking issue.
	// Each error span has a custom["issue"]["id"] field set by Datadog Error Tracking.
	query := fmt.Sprintf("service:%s status:error @issue.id:%s", service, issueID)
	if env != "" {
		query += " env:" + env
	}

	if c.verbose {
		fmt.Fprintf(os.Stderr, "[datadog] FetchIssueContext: spans query=%q from=%s to=%s\n",
			query, from.Format(time.RFC3339), to.Format(time.RFC3339))
	}

	body := datadogV2.SpansListRequest{
		Data: &datadogV2.SpansListRequestData{
			Attributes: &datadogV2.SpansListRequestAttributes{
				Filter: &datadogV2.SpansQueryFilter{
					Query: datadog.PtrString(query),
					From:  datadog.PtrString(from.Format(time.RFC3339)),
					To:    datadog.PtrString(to.Format(time.RFC3339)),
				},
				Page: &datadogV2.SpansListRequestPage{
					Limit: datadog.PtrInt32(3),
				},
			},
			Type: datadogV2.SPANSLISTREQUESTTYPE_SEARCH_REQUEST.Ptr(),
		},
	}

	resp, _, err := c.spansAPI.ListSpans(c.ctx(), body)
	if err != nil {
		if c.verbose {
			fmt.Fprintf(os.Stderr, "[datadog] FetchIssueContext: spans lookup failed (non-fatal): %v\n", err)
		}
		return ctx, nil
	}

	spans := resp.GetData()
	if c.verbose {
		fmt.Fprintf(os.Stderr, "[datadog] FetchIssueContext: got %d spans\n", len(spans))
	}

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

		if ctx.StackTrace == "" {
			custom := attrs.GetCustom()
			if custom != nil {
				if errVal, ok := custom["error"]; ok {
					if errMap, ok := errVal.(map[string]interface{}); ok {
						if stack, ok := errMap["stack"].(string); ok && stack != "" {
							ctx.StackTrace = stack
						}
					}
				}
			}
		}
	}

	if c.verbose {
		fmt.Fprintf(os.Stderr, "[datadog] FetchIssueContext: stackTrace found=%v traces=%d\n",
			ctx.StackTrace != "", len(ctx.Traces))
	}

	return ctx, nil
}

func mapKeys(m map[string]interface{}) []string {
	if m == nil {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// OverrideServers replaces the SDK server configuration (used for testing with httptest).
func (c *Client) OverrideServers(servers datadog.ServerConfigurations) {
	c.cfg.Servers = servers
}

func (c *Client) SearchLogs(queryStr, since string) ([]LogEntry, error) {
	// TODO: migrate to SDK Logs API when needed
	return nil, nil
}

// RateLimitInfo holds parsed rate limit headers from a Datadog API response.
type RateLimitInfo struct {
	Limit     int           // x-ratelimit-limit: max requests per period
	Period    int           // x-ratelimit-period: period in seconds
	Remaining int           // x-ratelimit-remaining: requests left in current period
	Reset     time.Duration // x-ratelimit-reset: seconds until limit resets
	Name      string        // x-ratelimit-name: which rate limit bucket
}

// RateLimitCallback is called after each response with parsed rate limit headers.
// Only called when headers are present.
type RateLimitCallback func(info RateLimitInfo)

// bearerTransport adds an Authorization: Bearer header to all requests
// and optionally parses rate limit headers from responses.
type bearerTransport struct {
	token         string
	base          http.RoundTripper
	onRateLimit   RateLimitCallback
}

func (t *bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+t.token)
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return resp, err
	}

	// Parse rate limit headers if callback is set and headers are present.
	if t.onRateLimit != nil && resp.Header.Get("X-Ratelimit-Limit") != "" {
		info := RateLimitInfo{
			Name: resp.Header.Get("X-Ratelimit-Name"),
		}
		fmt.Sscanf(resp.Header.Get("X-Ratelimit-Limit"), "%d", &info.Limit)
		fmt.Sscanf(resp.Header.Get("X-Ratelimit-Period"), "%d", &info.Period)
		fmt.Sscanf(resp.Header.Get("X-Ratelimit-Remaining"), "%d", &info.Remaining)
		var resetSec int
		fmt.Sscanf(resp.Header.Get("X-Ratelimit-Reset"), "%d", &resetSec)
		info.Reset = time.Duration(resetSec) * time.Second
		t.onRateLimit(info)
	}

	return resp, err
}

// SetRateLimitCallback sets a function to be called with rate limit info from responses.
func (c *Client) SetRateLimitCallback(cb RateLimitCallback) {
	if bt, ok := c.cfg.HTTPClient.Transport.(*bearerTransport); ok {
		bt.onRateLimit = cb
	}
}
