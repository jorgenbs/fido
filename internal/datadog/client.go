package datadog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type Client struct {
	token   string
	baseURL string
	verbose bool
	http    *http.Client
}

type ErrorIssue struct {
	ID         string               `json:"id"`
	Attributes ErrorIssueAttributes `json:"attributes"`
}

type ErrorIssueAttributes struct {
	Title      string `json:"title"`
	Message    string `json:"message"`
	Service    string `json:"service"`
	Env        string `json:"env"`
	FirstSeen  string `json:"first_seen"`
	LastSeen   string `json:"last_seen"`
	Count      int64  `json:"count"`
	Status     string `json:"status"`
	StackTrace string `json:"stack_trace,omitempty"`
}

type SearchIssuesResponse struct {
	Data []ErrorIssue `json:"data"`
}

type LogEntry struct {
	Attributes LogAttributes `json:"attributes"`
}

type LogAttributes struct {
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
	Service   string `json:"service"`
	Status    string `json:"status"`
}

type SearchLogsResponse struct {
	Data []LogEntry `json:"data"`
}

// searchRequest is the request body for POST /api/v2/error-tracking/issues/search
type searchRequest struct {
	Data searchRequestData `json:"data"`
}

type searchRequestData struct {
	Type       string                   `json:"type"`
	Attributes searchRequestAttributes `json:"attributes"`
}

type searchRequestAttributes struct {
	From  int64  `json:"from"`
	To    int64  `json:"to"`
	Query string `json:"query"`
}

func NewClient(token, baseURL string) *Client {
	return &Client{
		token:   token,
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) SetVerbose(v bool) { c.verbose = v }

func (c *Client) doRequest(method, path string, query url.Values, reqBody io.Reader) ([]byte, error) {
	u := fmt.Sprintf("%s%s", c.baseURL, path)
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	if c.verbose {
		fmt.Fprintf(os.Stderr, "[datadog] %s %s\n", method, u)
	}

	req, err := http.NewRequest(method, u, reqBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if c.verbose {
		fmt.Fprintf(os.Stderr, "[datadog] %d %s (%d bytes)\n", resp.StatusCode, resp.Status, len(body))
		if resp.StatusCode >= 400 {
			fmt.Fprintf(os.Stderr, "[datadog] response: %s\n", string(body))
		}
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	return body, nil
}

func (c *Client) SearchErrorIssues(services []string, since string) ([]ErrorIssue, error) {
	// Parse the "since" duration (e.g. "24h") into a time range
	dur, err := time.ParseDuration(since)
	if err != nil {
		return nil, fmt.Errorf("invalid since duration %q: %w", since, err)
	}

	now := time.Now()
	from := now.Add(-dur)

	// Build the search query
	queryParts := []string{}
	for _, svc := range services {
		queryParts = append(queryParts, "service:"+svc)
	}
	queryStr := strings.Join(queryParts, " OR ")

	reqBody := searchRequest{
		Data: searchRequestData{
			Type: "search_request",
			Attributes: searchRequestAttributes{
				From:  from.UnixMilli(),
				To:    now.UnixMilli(),
				Query: queryStr,
			},
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	if c.verbose {
		fmt.Fprintf(os.Stderr, "[datadog] request body: %s\n", string(bodyBytes))
	}

	respBody, err := c.doRequest("POST", "/api/v2/error-tracking/issues/search", nil, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}

	var resp SearchIssuesResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	return resp.Data, nil
}

func (c *Client) SearchLogs(queryStr, since string) ([]LogEntry, error) {
	query := url.Values{}
	query.Set("filter[query]", queryStr)
	query.Set("filter[from]", since)

	body, err := c.doRequest("GET", "/api/v2/logs/events", query, nil)
	if err != nil {
		return nil, err
	}

	var resp SearchLogsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	return resp.Data, nil
}
