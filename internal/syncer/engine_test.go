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
	stackCount  atomic.Int32
	issues      []ScanResult
}

func (m *mockDeps) ScanIssues() ([]ScanResult, error) {
	m.scanCount.Add(1)
	return m.issues, nil
}
func (m *mockDeps) FetchStacktrace(issueID, service, env, firstSeen, lastSeen string) (string, error) {
	m.stackCount.Add(1)
	return "stack trace here", nil
}
func (m *mockDeps) SaveStacktrace(issueID, stacktrace string) error                      { return nil }
func (m *mockDeps) HasStacktrace(issueID string) bool                                     { return false }
func (m *mockDeps) Publish(eventType string, payload map[string]any)                      {}
func (m *mockDeps) ListTrackedIssues() ([]TrackedIssue, error)                            { return nil, nil }
func (m *mockDeps) ResolveIssue(_ string) error                                           { return nil }
func (m *mockDeps) GetIssueStatus(_ string) (string, error)                               { return "", nil }
func (m *mockDeps) SetDatadogStatus(_, _, _ string) error                                 { return nil }
func (m *mockDeps) IncrementRegressionCount(_ string) error                               { return nil }

func TestEngine_RunsAndEnqueuesFollowUpJobs(t *testing.T) {
	deps := &mockDeps{
		issues: []ScanResult{
			{IssueID: "issue-1", Service: "svc-a", Env: "prod", FirstSeen: "2026-04-03T10:00:00Z", LastSeen: "2026-04-03T14:00:00Z"},
		},
	}

	eng := NewEngine(deps, EngineConfig{
		Interval:  100 * time.Millisecond,
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
	total := deps.stackCount.Load()
	if total >= 20 {
		t.Errorf("rate limiter not working: %d follow-up jobs completed (expected < 20)", total)
	}
}

// --- resolve_check test infrastructure ---

type resolveCheckDeps struct {
	trackedIssues    []TrackedIssue
	resolvedIssueIDs []string
	statusByIssueID  map[string]string
	setStatusCalls   []setStatusCall
	regressionCounts map[string]int
	publishedEvents  []publishedEvent
}

type setStatusCall struct {
	IssueID    string
	Status     string
	ResolvedAt string
}

type publishedEvent struct {
	EventType string
	Payload   map[string]any
}

func (d *resolveCheckDeps) ScanIssues() ([]ScanResult, error)                       { return nil, nil }
func (d *resolveCheckDeps) FetchStacktrace(_, _, _, _, _ string) (string, error)     { return "", nil }
func (d *resolveCheckDeps) SaveStacktrace(_, _ string) error                         { return nil }
func (d *resolveCheckDeps) HasStacktrace(_ string) bool                              { return true }
func (d *resolveCheckDeps) ListTrackedIssues() ([]TrackedIssue, error)               { return d.trackedIssues, nil }

func (d *resolveCheckDeps) ResolveIssue(datadogIssueID string) error {
	d.resolvedIssueIDs = append(d.resolvedIssueIDs, datadogIssueID)
	return nil
}

func (d *resolveCheckDeps) GetIssueStatus(datadogIssueID string) (string, error) {
	if s, ok := d.statusByIssueID[datadogIssueID]; ok {
		return s, nil
	}
	return "OPEN", nil
}

func (d *resolveCheckDeps) SetDatadogStatus(issueID, status, resolvedAt string) error {
	d.setStatusCalls = append(d.setStatusCalls, setStatusCall{issueID, status, resolvedAt})
	return nil
}

func (d *resolveCheckDeps) IncrementRegressionCount(issueID string) error {
	if d.regressionCounts == nil {
		d.regressionCounts = map[string]int{}
	}
	d.regressionCounts[issueID]++
	return nil
}

func (d *resolveCheckDeps) Publish(eventType string, payload map[string]any) {
	d.publishedEvents = append(d.publishedEvents, publishedEvent{eventType, payload})
}

// --- resolve_check tests ---

func TestEngine_ResolveCheck_MRMergedTriggersResolve(t *testing.T) {
	deps := &resolveCheckDeps{
		trackedIssues: []TrackedIssue{
			{
				IssueID:        "issue-1",
				DatadogIssueID: "dd-abc",
				MRStatus:       "merged",
				DatadogStatus:  "",
				ResolvedAt:     "",
				Ignored:        false,
			},
		},
		statusByIssueID: map[string]string{"dd-abc": "RESOLVED"},
	}

	e := NewEngine(deps, EngineConfig{Interval: time.Minute, RateLimit: 60})
	e.executeResolveCheck()

	if len(deps.resolvedIssueIDs) != 1 || deps.resolvedIssueIDs[0] != "dd-abc" {
		t.Errorf("expected ResolveIssue called with dd-abc, got %v", deps.resolvedIssueIDs)
	}
	if len(deps.setStatusCalls) < 1 {
		t.Fatal("expected SetDatadogStatus to be called")
	}
	call := deps.setStatusCalls[0]
	if call.Status != "resolved" {
		t.Errorf("expected status=resolved, got %s", call.Status)
	}
	if call.ResolvedAt == "" {
		t.Error("expected ResolvedAt to be set")
	}
}

func TestEngine_ResolveCheck_DetectsRegression(t *testing.T) {
	deps := &resolveCheckDeps{
		trackedIssues: []TrackedIssue{
			{
				IssueID:        "issue-1",
				DatadogIssueID: "dd-abc",
				MRStatus:       "merged",
				DatadogStatus:  "resolved",
				ResolvedAt:     "2026-04-09T12:00:00Z",
				Ignored:        false,
			},
		},
		statusByIssueID: map[string]string{"dd-abc": "OPEN"},
	}

	e := NewEngine(deps, EngineConfig{Interval: time.Minute, RateLimit: 60})
	e.executeResolveCheck()

	// Should NOT call ResolveIssue again
	if len(deps.resolvedIssueIDs) != 0 {
		t.Errorf("expected no ResolveIssue calls, got %v", deps.resolvedIssueIDs)
	}

	// Should detect regression
	if deps.regressionCounts["issue-1"] != 1 {
		t.Errorf("expected regression count 1, got %d", deps.regressionCounts["issue-1"])
	}

	// Should update status to open
	found := false
	for _, c := range deps.setStatusCalls {
		if c.IssueID == "issue-1" && c.Status == "open" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected SetDatadogStatus(issue-1, open), got %v", deps.setStatusCalls)
	}

	// Should publish regression event
	hasRegressionEvent := false
	for _, ev := range deps.publishedEvents {
		if ev.EventType == "issue:regression" {
			hasRegressionEvent = true
		}
	}
	if !hasRegressionEvent {
		t.Errorf("expected issue:regression event, got %v", deps.publishedEvents)
	}
}

func TestEngine_ResolveCheck_NoReResolveWhenAlreadyResolved(t *testing.T) {
	deps := &resolveCheckDeps{
		trackedIssues: []TrackedIssue{
			{
				IssueID:        "issue-1",
				DatadogIssueID: "dd-abc",
				MRStatus:       "merged",
				DatadogStatus:  "resolved",
				ResolvedAt:     "2026-04-09T12:00:00Z",
				Ignored:        false,
			},
		},
		statusByIssueID: map[string]string{"dd-abc": "RESOLVED"},
	}

	e := NewEngine(deps, EngineConfig{Interval: time.Minute, RateLimit: 60})
	e.executeResolveCheck()

	if len(deps.resolvedIssueIDs) != 0 {
		t.Errorf("expected no ResolveIssue calls, got %v", deps.resolvedIssueIDs)
	}
	if len(deps.setStatusCalls) != 0 {
		t.Errorf("expected no status updates, got %v", deps.setStatusCalls)
	}
}

func TestEngine_ResolveCheck_SkipsIgnoredIssues(t *testing.T) {
	deps := &resolveCheckDeps{
		trackedIssues: []TrackedIssue{
			{
				IssueID:        "issue-1",
				DatadogIssueID: "dd-abc",
				MRStatus:       "merged",
				DatadogStatus:  "",
				ResolvedAt:     "",
				Ignored:        true,
			},
		},
		statusByIssueID: map[string]string{"dd-abc": "OPEN"},
	}

	e := NewEngine(deps, EngineConfig{Interval: time.Minute, RateLimit: 60})
	e.executeResolveCheck()

	if len(deps.resolvedIssueIDs) != 0 {
		t.Errorf("expected no API calls for ignored issue, got %v", deps.resolvedIssueIDs)
	}
}

func TestEngine_ResolveCheck_StatusSyncNonRegression(t *testing.T) {
	deps := &resolveCheckDeps{
		trackedIssues: []TrackedIssue{
			{
				IssueID:        "issue-1",
				DatadogIssueID: "dd-abc",
				MRStatus:       "",
				DatadogStatus:  "open",
				ResolvedAt:     "",
				Ignored:        false,
			},
		},
		statusByIssueID: map[string]string{"dd-abc": "ACKNOWLEDGED"},
	}

	e := NewEngine(deps, EngineConfig{Interval: time.Minute, RateLimit: 60})
	e.executeResolveCheck()

	found := false
	for _, c := range deps.setStatusCalls {
		if c.IssueID == "issue-1" && c.Status == "acknowledged" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected status update to acknowledged, got %v", deps.setStatusCalls)
	}

	for _, ev := range deps.publishedEvents {
		if ev.EventType == "issue:regression" {
			t.Error("should not fire regression event for non-regression status change")
		}
	}
}
