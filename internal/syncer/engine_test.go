package syncer

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

type mockDeps struct {
	scanCount   atomic.Int32
	bucketCount atomic.Int32
	stackCount  atomic.Int32
	issues      []ScanResult
}

func (m *mockDeps) ScanIssues() ([]ScanResult, error) {
	m.scanCount.Add(1)
	return m.issues, nil
}
func (m *mockDeps) FetchBuckets(issueID, service, env, window string) ([]BucketData, error) {
	m.bucketCount.Add(1)
	return []BucketData{{Timestamp: "2026-04-03T10:00:00Z", Count: 5}}, nil
}
func (m *mockDeps) FetchStacktrace(issueID, service, env, firstSeen, lastSeen string) (string, error) {
	m.stackCount.Add(1)
	return "stack trace here", nil
}
func (m *mockDeps) SaveBuckets(issueID string, buckets []BucketData, window string) error { return nil }
func (m *mockDeps) SaveStacktrace(issueID, stacktrace string) error                      { return nil }
func (m *mockDeps) IsBucketStale(issueID, window string, maxAge time.Duration) bool       { return true }
func (m *mockDeps) HasStacktrace(issueID string) bool                                     { return false }
func (m *mockDeps) Publish(eventType string, payload map[string]any)                      {}

func TestEngine_RunsAndEnqueuesFollowUpJobs(t *testing.T) {
	deps := &mockDeps{
		issues: []ScanResult{
			{IssueID: "issue-1", Service: "svc-a", Env: "prod", FirstSeen: "2026-04-03T10:00:00Z", LastSeen: "2026-04-03T14:00:00Z"},
		},
	}

	eng := NewEngine(deps, EngineConfig{
		Interval:  100 * time.Millisecond,
		Window:    "24h",
		RateLimit: 600,
	})

	// Simulate server feedback: generous limit so follow-up jobs can proceed.
	eng.Limiter().Update(600, 600, time.Minute, time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
	defer cancel()

	eng.Run(ctx)

	if deps.scanCount.Load() < 2 {
		t.Errorf("expected at least 2 scans, got %d", deps.scanCount.Load())
	}
	if deps.bucketCount.Load() < 1 {
		t.Errorf("expected at least 1 bucket fetch, got %d", deps.bucketCount.Load())
	}
	if deps.stackCount.Load() < 1 {
		t.Errorf("expected at least 1 stacktrace fetch, got %d", deps.stackCount.Load())
	}
}

func TestEngine_RespectsRateLimit(t *testing.T) {
	issues := make([]ScanResult, 20)
	for i := range issues {
		issues[i] = ScanResult{
			IssueID: fmt.Sprintf("issue-%d", i),
			Service: "svc", Env: "prod",
			FirstSeen: "2026-04-03T10:00:00Z", LastSeen: "2026-04-03T14:00:00Z",
		}
	}
	deps := &mockDeps{issues: issues}

	eng := NewEngine(deps, EngineConfig{
		Interval:  50 * time.Millisecond,
		Window:    "24h",
		RateLimit: 6,
	})

	// Simulate server feedback: limit=3, remaining=3, period=60s, reset=60s
	// Only 3 requests allowed in the window.
	eng.Limiter().Update(3, 3, 60*time.Second, 60*time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	eng.Run(ctx)

	// 20 issues × 2 follow-ups = 40 jobs. With limit of 3 per 60s,
	// only ~3 should complete in 200ms.
	total := deps.bucketCount.Load() + deps.stackCount.Load()
	if total >= 20 {
		t.Errorf("rate limiter not working: %d follow-up jobs completed (expected < 20)", total)
	}
}
