// internal/syncer/engine.go
package syncer

import (
	"context"
	"log"
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

// BucketData matches the reports.Bucket type to avoid circular imports.
type BucketData struct {
	Timestamp string
	Count     int64
}

// Deps abstracts the external dependencies the engine needs.
type Deps interface {
	ScanIssues() ([]ScanResult, error)
	FetchBuckets(issueID, service, env, window string) ([]BucketData, error)
	FetchStacktrace(issueID, service, env, firstSeen, lastSeen string) (string, error)
	SaveBuckets(issueID string, buckets []BucketData, window string) error
	SaveStacktrace(issueID, stacktrace string) error
	IsBucketStale(issueID, window string, maxAge time.Duration) bool
	HasStacktrace(issueID string) bool
	Publish(eventType string, payload map[string]any)
}

// EngineConfig holds runtime settings.
type EngineConfig struct {
	Interval  time.Duration
	Window    string
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
		case JobFetchBuckets:
			if err := e.limiter.WaitContext(ctx); err != nil {
				return
			}
			e.executeFetchBuckets(job)
		case JobFetchStacktrace:
			if err := e.limiter.WaitContext(ctx); err != nil {
				return
			}
			e.executeFetchStacktrace(job)
		case JobResolveCheck:
			if err := e.limiter.WaitContext(ctx); err != nil {
				return
			}
			log.Printf("syncer: resolve_check for issue %s: not yet implemented", job.IssueID)
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
		// Store metadata so follow-up jobs have access to service/env/timestamps.
		e.scanMeta[r.IssueID] = r

		if e.deps.IsBucketStale(r.IssueID, e.config.Window, 30*time.Minute) {
			e.queue.Push(Job{Type: JobFetchBuckets, IssueID: r.IssueID, Priority: 2})
		}
		if !e.deps.HasStacktrace(r.IssueID) {
			e.queue.Push(Job{Type: JobFetchStacktrace, IssueID: r.IssueID, Priority: 3})
		}
	}

	e.deps.Publish("scan:complete", map[string]any{"count": len(results)})
}

// executeFetchBuckets fetches bucket data for an issue and saves it.
func (e *Engine) executeFetchBuckets(job Job) {
	meta, ok := e.scanMeta[job.IssueID]
	if !ok {
		log.Printf("syncer: no metadata for issue %s, skipping fetch_buckets", job.IssueID)
		return
	}

	buckets, err := e.deps.FetchBuckets(job.IssueID, meta.Service, meta.Env, e.config.Window)
	if err != nil {
		log.Printf("syncer: FetchBuckets error for %s: %v", job.IssueID, err)
		return
	}

	if err := e.deps.SaveBuckets(job.IssueID, buckets, e.config.Window); err != nil {
		log.Printf("syncer: SaveBuckets error for %s: %v", job.IssueID, err)
		return
	}

	e.deps.Publish("issue:updated", map[string]any{"issueID": job.IssueID, "type": "buckets"})
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
