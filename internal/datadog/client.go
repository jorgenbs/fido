package datadog

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/DataDog/datadog-api-client-go/v2/api/datadog"
	"github.com/DataDog/datadog-api-client-go/v2/api/datadogV2"
)

type Client struct {
	token   string
	site    string
	verbose bool
	api     *datadogV2.ErrorTrackingApi
	cfg     *datadog.Configuration
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

func NewClient(token, site string) (*Client, error) {
	if token == "" {
		return nil, fmt.Errorf("datadog token is required")
	}
	if site == "" {
		return nil, fmt.Errorf("datadog site is required")
	}

	cfg := datadog.NewConfiguration()

	c := &Client{
		token: token,
		site:  site,
		cfg:   cfg,
	}

	// Inject a custom transport that adds Bearer auth for PAT
	cfg.HTTPClient = &http.Client{
		Timeout:   30 * time.Second,
		Transport: &bearerTransport{token: token, base: http.DefaultTransport},
	}

	apiClient := datadog.NewAPIClient(cfg)
	c.api = datadogV2.NewErrorTrackingApi(apiClient)

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
