package syncer

import (
	"fmt"
	"strings"
	"github.com/jorgenbs/fido/internal/api"
	"github.com/jorgenbs/fido/internal/datadog"
	"github.com/jorgenbs/fido/internal/reports"
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

	ctx, err := a.ddClient.FetchIssueContext(issueID, service, env, firstSeen, lastSeen)
	if err != nil {
		return "", err
	}
	return ctx.StackTrace, nil
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

func (a *Adapter) ListTrackedIssues() ([]TrackedIssue, error) {
	issues, err := a.mgr.ListIssues(false)
	if err != nil {
		return nil, err
	}

	var tracked []TrackedIssue
	for _, issue := range issues {
		ti := TrackedIssue{
			IssueID: issue.ID,
		}
		if issue.Meta != nil {
			ti.DatadogStatus = issue.Meta.DatadogStatus
			ti.ResolvedAt = issue.Meta.ResolvedAt
			ti.Ignored = issue.Meta.Ignored
		}
		if resolve, err := a.mgr.ReadResolve(issue.ID); err == nil {
			ti.MRStatus = resolve.MRStatus
			ti.DatadogIssueID = resolve.DatadogIssueID
		}
		// Fall back to using the issue ID as the Datadog issue ID
		if ti.DatadogIssueID == "" {
			ti.DatadogIssueID = issue.ID
		}
		tracked = append(tracked, ti)
	}
	return tracked, nil
}

func (a *Adapter) ResolveIssue(datadogIssueID string) error {
	return a.ddClient.ResolveIssue(datadogIssueID)
}

func (a *Adapter) GetIssueStatus(datadogIssueID string) (string, error) {
	return a.ddClient.GetIssueStatus(datadogIssueID)
}

func (a *Adapter) SetDatadogStatus(issueID, status, resolvedAt string) error {
	return a.mgr.SetDatadogStatus(issueID, status, resolvedAt)
}

func (a *Adapter) IncrementRegressionCount(issueID string) error {
	return a.mgr.IncrementRegressionCount(issueID)
}
