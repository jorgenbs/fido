// internal/syncer/engine.go
package syncer

import (
	"context"
	"log"
	"strings"
	"time"
)

// ScanResult is the output of a scan: one entry per issue found.
type ScanResult struct {
	IssueID       string
	Service       string
	Env           string
	FirstSeen     string
	LastSeen      string
	HasStacktrace bool
}

// TrackedIssue is a summary of a tracked issue for resolve_check jobs.
type TrackedIssue struct {
	IssueID        string
	DatadogIssueID string
	MRStatus       string
	DatadogStatus  string
	ResolvedAt     string
	Ignored        bool
}

// Deps abstracts the external dependencies the engine needs.
type Deps interface {
	ScanIssues() ([]ScanResult, error)
	FetchStacktrace(issueID, service, env, firstSeen, lastSeen string) (string, error)
	SaveStacktrace(issueID, stacktrace string) error
	HasStacktrace(issueID string) bool
	Publish(eventType string, payload map[string]any)

	// Resolution lifecycle
	ListTrackedIssues() ([]TrackedIssue, error)
	ResolveIssue(datadogIssueID string) error
	GetIssueStatus(datadogIssueID string) (string, error)
	SetDatadogStatus(issueID, status, resolvedAt string) error
	IncrementRegressionCount(issueID string) error
}

// EngineConfig holds runtime settings.
type EngineConfig struct {
	Interval  time.Duration
	RateLimit int
}

// Engine is the daemon sync engine.
type Engine struct {
	deps    Deps
	config  EngineConfig
	queue   *JobQueue
	limiter *RateLimiter

	// scanMeta stores per-issue metadata needed for follow-up jobs.
	// Keyed by issueID.
	scanMeta map[string]ScanResult
}

// NewEngine creates a new sync engine.
func NewEngine(deps Deps, cfg EngineConfig) *Engine {
	rateLimit := cfg.RateLimit
	if rateLimit <= 0 {
		rateLimit = 60
	}
	limiter := NewRateLimiter(rateLimit)

	return &Engine{
		deps:     deps,
		config:   cfg,
		queue:    NewJobQueue(),
		limiter:  limiter,
		scanMeta: make(map[string]ScanResult),
	}
}

// Limiter returns the engine's rate limiter so external code can feed it
// server-reported rate limit headers.
func (e *Engine) Limiter() *RateLimiter {
	return e.limiter
}

// EnqueueIssue stores metadata for an issue and enqueues follow-up jobs
// (e.g. stacktrace fetch). Use this when an issue is imported outside
// the normal scan cycle.
func (e *Engine) EnqueueIssue(result ScanResult) {
	e.scanMeta[result.IssueID] = result
	if !e.deps.HasStacktrace(result.IssueID) {
		e.queue.Push(Job{Type: JobFetchStacktrace, IssueID: result.IssueID, Priority: 2})
	}
}

// Run blocks until ctx is cancelled. It runs an initial sync, starts a worker
// goroutine to drain the queue, and enqueues sync_issues on each interval tick.
func (e *Engine) Run(ctx context.Context) {
	// Initial sync immediately.
	e.queue.Push(Job{Type: JobSyncIssues, Priority: 1})

	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		e.worker(ctx)
	}()

	ticker := time.NewTicker(e.config.Interval)
	defer ticker.Stop()

loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case <-ticker.C:
			e.queue.Push(Job{Type: JobSyncIssues, Priority: 1})
		}
	}

	<-workerDone
}

// worker continuously drains the job queue until ctx is cancelled.
func (e *Engine) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		job := e.queue.Pop()
		if job.Type == "" {
			// Queue empty — yield briefly to avoid busy spin.
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Millisecond):
			}
			continue
		}

		switch job.Type {
		case JobSyncIssues:
			// sync_issues runs without rate limiting (one per cycle).
			e.executeSyncIssues()
		case JobFetchStacktrace:
			if err := e.limiter.WaitContext(ctx); err != nil {
				return
			}
			e.executeFetchStacktrace(job)
		case JobResolveCheck:
			// resolve_check processes all tracked issues, not individual ones
			e.executeResolveCheck()
		default:
			log.Printf("syncer: unknown job type %q, skipping", job.Type)
		}
	}
}

// executeSyncIssues calls ScanIssues and enqueues follow-up jobs for each result.
func (e *Engine) executeSyncIssues() {
	results, err := e.deps.ScanIssues()
	if err != nil {
		log.Printf("syncer: ScanIssues error: %v", err)
		return
	}

	for _, r := range results {
		e.scanMeta[r.IssueID] = r

		if !e.deps.HasStacktrace(r.IssueID) {
			e.queue.Push(Job{Type: JobFetchStacktrace, IssueID: r.IssueID, Priority: 3})
		}
	}

	// Enqueue resolve_check to run after stacktrace fetches
	e.queue.Push(Job{Type: JobResolveCheck, Priority: 4})

	e.deps.Publish("scan:complete", map[string]any{"count": len(results)})
}

// executeFetchStacktrace fetches a stack trace for an issue and saves it.
func (e *Engine) executeFetchStacktrace(job Job) {
	meta, ok := e.scanMeta[job.IssueID]
	if !ok {
		log.Printf("syncer: no metadata for issue %s, skipping fetch_stacktrace", job.IssueID)
		return
	}

	stacktrace, err := e.deps.FetchStacktrace(job.IssueID, meta.Service, meta.Env, meta.FirstSeen, meta.LastSeen)
	if err != nil {
		log.Printf("syncer: FetchStacktrace error for %s: %v", job.IssueID, err)
		return
	}

	if err := e.deps.SaveStacktrace(job.IssueID, stacktrace); err != nil {
		log.Printf("syncer: SaveStacktrace error for %s: %v", job.IssueID, err)
		return
	}

	e.deps.Publish("issue:updated", map[string]any{"issueID": job.IssueID, "type": "stacktrace"})
}

// executeResolveCheck runs the resolution lifecycle logic for all tracked issues.
func (e *Engine) executeResolveCheck() {
	issues, err := e.deps.ListTrackedIssues()
	if err != nil {
		log.Printf("syncer: ListTrackedIssues error: %v", err)
		return
	}

	for _, issue := range issues {
		if issue.Ignored {
			continue
		}
		if issue.DatadogIssueID == "" {
			continue
		}

		// Step 1: MR merge -> resolve in Datadog (once per fix cycle)
		if issue.MRStatus == "merged" && issue.ResolvedAt == "" {
			if err := e.deps.ResolveIssue(issue.DatadogIssueID); err != nil {
				log.Printf("syncer: ResolveIssue error for %s: %v", issue.IssueID, err)
				continue
			}
			now := time.Now().UTC().Format(time.RFC3339)
			if err := e.deps.SetDatadogStatus(issue.IssueID, "resolved", now); err != nil {
				log.Printf("syncer: SetDatadogStatus error for %s: %v", issue.IssueID, err)
			}
			e.deps.Publish("issue:resolved", map[string]any{"id": issue.IssueID})
			continue // skip status check this cycle
		}

		// Step 2: Check current Datadog status
		ddStatus, err := e.deps.GetIssueStatus(issue.DatadogIssueID)
		if err != nil {
			log.Printf("syncer: GetIssueStatus error for %s: %v", issue.IssueID, err)
			continue
		}

		normalizedStatus := strings.ToLower(ddStatus)
		storedStatus := strings.ToLower(issue.DatadogStatus)

		if normalizedStatus == storedStatus {
			continue
		}

		// Status diverged
		isRegression := storedStatus == "resolved" && (normalizedStatus == "open" || normalizedStatus == "for_review")
		if isRegression {
			if err := e.deps.IncrementRegressionCount(issue.IssueID); err != nil {
				log.Printf("syncer: IncrementRegressionCount error for %s: %v", issue.IssueID, err)
			}
			if err := e.deps.SetDatadogStatus(issue.IssueID, normalizedStatus, ""); err != nil {
				log.Printf("syncer: SetDatadogStatus error for %s: %v", issue.IssueID, err)
			}
			e.deps.Publish("issue:regression", map[string]any{"id": issue.IssueID})
		} else {
			if err := e.deps.SetDatadogStatus(issue.IssueID, normalizedStatus, ""); err != nil {
				log.Printf("syncer: SetDatadogStatus error for %s: %v", issue.IssueID, err)
			}
			e.deps.Publish("issue:status_changed", map[string]any{"id": issue.IssueID, "status": normalizedStatus})
		}
	}
}
