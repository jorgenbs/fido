package datadog

import (
	"context"
	"fmt"
	"net/http"
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

// IssueContext holds deep-link URLs and trace references for a Datadog issue.
type IssueContext struct {
	Traces    []TraceRef
	EventsURL string
	TracesURL string
}

// TraceRef is a reference to a single Datadog trace.
type TraceRef struct {
	TraceID string
	URL     string
}

// FetchIssueContext builds deep-link URLs and attempts to fetch sample traces
// for the given service/env within the time window defined by firstSeen/lastSeen.
func (c *Client) FetchIssueContext(service, env, firstSeen, lastSeen string) (IssueContext, error) {
	from, _ := time.Parse(time.RFC3339, firstSeen)
	to, _ := time.Parse(time.RFC3339, lastSeen)
	from = from.Add(-5 * time.Minute)
	to = to.Add(5 * time.Minute)

	eventsURL := fmt.Sprintf(
		"https://%s.%s/event/explorer?query=service:%s env:%s&from=%d&to=%d",
		c.orgSubdomain, c.site, service, env, from.UnixMilli(), to.UnixMilli(),
	)
	tracesURL := fmt.Sprintf(
		"https://%s.%s/apm/traces?query=service:%s env:%s&start=%d&end=%d",
		c.orgSubdomain, c.site, service, env, from.UnixMilli(), to.UnixMilli(),
	)

	ctx := IssueContext{
		EventsURL: eventsURL,
		TracesURL: tracesURL,
	}

	query := fmt.Sprintf("service:%s", service)
	if env != "" {
		query += " env:" + env
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
			fmt.Fprintf(os.Stderr, "FetchIssueContext: spans lookup failed (non-fatal): %v\n", err)
		}
		return ctx, nil
	}

	for _, span := range resp.GetData() {
		attrs := span.GetAttributes()
		traceID := attrs.GetTraceId()
		if traceID == "" {
			continue
		}
		traceURL := fmt.Sprintf("https://%s.%s/apm/trace/%s", c.orgSubdomain, c.site, traceID)
		ctx.Traces = append(ctx.Traces, TraceRef{TraceID: traceID, URL: traceURL})
	}

	return ctx, nil
}

// OverrideServers replaces the SDK server configuration (used for testing with httptest).
func (c *Client) OverrideServers(servers datadog.ServerConfigurations) {
	c.cfg.Servers = servers
}

func (c *Client) SearchLogs(queryStr, since string) ([]LogEntry, error) {
	// TODO: migrate to SDK Logs API when needed
	return nil, nil
}

// bearerTransport adds an Authorization: Bearer header to all requests.
type bearerTransport struct {
	token string
	base  http.RoundTripper
}

func (t *bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(req)
}
