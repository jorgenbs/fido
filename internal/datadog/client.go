package datadog

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	token   string
	baseURL string
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

func NewClient(token, baseURL string) *Client {
	return &Client{
		token:   token,
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) do(method, path string, query url.Values) ([]byte, error) {
	u := fmt.Sprintf("%s%s", c.baseURL, path)
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	req, err := http.NewRequest(method, u, nil)
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

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	return body, nil
}

func (c *Client) SearchErrorIssues(services []string, since string) ([]ErrorIssue, error) {
	query := url.Values{}
	if len(services) > 0 {
		query.Set("filter[services]", strings.Join(services, ","))
	}
	query.Set("filter[since]", since)

	body, err := c.do("GET", "/api/v2/error-tracking/issues", query)
	if err != nil {
		return nil, err
	}

	var resp SearchIssuesResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	return resp.Data, nil
}

func (c *Client) SearchLogs(queryStr, since string) ([]LogEntry, error) {
	query := url.Values{}
	query.Set("filter[query]", queryStr)
	query.Set("filter[from]", since)

	body, err := c.do("GET", "/api/v2/logs/events", query)
	if err != nil {
		return nil, err
	}

	var resp SearchLogsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	return resp.Data, nil
}
