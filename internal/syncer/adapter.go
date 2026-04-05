package syncer

import (
	"fmt"
	"strings"
	"time"

	"github.com/ruter-as/fido/internal/api"
	"github.com/ruter-as/fido/internal/datadog"
	"github.com/ruter-as/fido/internal/reports"
)

// Adapter implements Deps by bridging the engine to the real Datadog client,
// reports manager, and SSE hub.
type Adapter struct {
	ddClient *datadog.Client
	mgr      *reports.Manager
	hub      *api.Hub
	scanFn   func() ([]ScanResult, error)
}

// NewAdapter creates a Deps adapter.
func NewAdapter(
	ddClient *datadog.Client,
	mgr *reports.Manager,
	hub *api.Hub,
	scanFn func() ([]ScanResult, error),
) *Adapter {
	return &Adapter{
		ddClient: ddClient,
		mgr:      mgr,
		hub:      hub,
		scanFn:   scanFn,
	}
}

func (a *Adapter) ScanIssues() ([]ScanResult, error) {
	return a.scanFn()
}

func (a *Adapter) FetchBuckets(issueID, service, env, window string) ([]BucketData, error) {
	if service == "" || env == "" {
		meta, err := a.mgr.ReadMetadata(issueID)
		if err != nil {
			return nil, fmt.Errorf("reading metadata for %s: %w", issueID, err)
		}
		if service == "" {
			service = meta.Service
		}
		if env == "" {
			env = meta.Env
		}
	}

	timeline, err := a.ddClient.FetchErrorTimeline(service, env, window)
	if err != nil {
		return nil, err
	}

	buckets := make([]BucketData, len(timeline))
	for i, tb := range timeline {
		buckets[i] = BucketData{Timestamp: tb.Timestamp, Count: tb.Count}
	}
	return buckets, nil
}

func (a *Adapter) FetchStacktrace(issueID, service, env, firstSeen, lastSeen string) (string, error) {
	if service == "" || firstSeen == "" || lastSeen == "" {
		meta, err := a.mgr.ReadMetadata(issueID)
		if err != nil {
			return "", fmt.Errorf("reading metadata for %s: %w", issueID, err)
		}
		if service == "" {
			service = meta.Service
		}
		if env == "" {
			env = meta.Env
		}
		if firstSeen == "" {
			firstSeen = meta.FirstSeen
		}
		if lastSeen == "" {
			lastSeen = meta.LastSeen
		}
	}

	ctx, err := a.ddClient.FetchIssueContext(service, env, firstSeen, lastSeen)
	if err != nil {
		return "", err
	}
	return ctx.StackTrace, nil
}

func (a *Adapter) SaveBuckets(issueID string, buckets []BucketData, window string) error {
	rBuckets := make([]reports.Bucket, len(buckets))
	for i, b := range buckets {
		rBuckets[i] = reports.Bucket{Timestamp: b.Timestamp, Count: b.Count}
	}
	ts := &reports.TimeSeries{
		Buckets:     rBuckets,
		Window:      window,
		LastFetched: time.Now().UTC().Format(time.RFC3339),
	}
	return a.mgr.WriteTimeSeries(issueID, ts)
}

func (a *Adapter) SaveStacktrace(issueID, stacktrace string) error {
	if stacktrace == "" {
		return nil // don't replace marker with empty content
	}
	content, err := a.mgr.ReadError(issueID)
	if err != nil {
		return err
	}
	replacement := "```\n" + stacktrace + "\n```"
	if strings.Contains(content, "<!-- STACK_TRACE_PENDING -->") {
		updated := strings.Replace(content, "<!-- STACK_TRACE_PENDING -->", replacement, 1)
		return a.mgr.WriteError(issueID, updated)
	}
	// Fix up empty code blocks left by a previous empty-stacktrace save
	if strings.Contains(content, "```\n\n```") {
		updated := strings.Replace(content, "```\n\n```", replacement, 1)
		return a.mgr.WriteError(issueID, updated)
	}
	return nil
}

func (a *Adapter) IsBucketStale(issueID, window string, maxAge time.Duration) bool {
	ts, err := a.mgr.ReadTimeSeries(issueID)
	if err != nil {
		return true // no cached data
	}
	return ts.IsStale(window, maxAge)
}

func (a *Adapter) HasStacktrace(issueID string) bool {
	content, err := a.mgr.ReadError(issueID)
	if err != nil {
		return false
	}
	if strings.Contains(content, "<!-- STACK_TRACE_PENDING -->") {
		return false
	}
	// Detect empty stack trace left by a previous empty replacement
	if strings.Contains(content, "```\n\n```") {
		return false
	}
	return true
}

func (a *Adapter) Publish(eventType string, payload map[string]any) {
	if a.hub != nil {
		a.hub.Publish(api.Event{Type: eventType, Payload: payload})
	}
}
