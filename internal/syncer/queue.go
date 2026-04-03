// internal/syncer/queue.go
package syncer

import (
	"container/heap"
	"sync"
)

// JobType identifies the kind of sync work to do.
type JobType string

const (
	JobSyncIssues      JobType = "sync_issues"
	JobFetchBuckets    JobType = "fetch_buckets"
	JobFetchStacktrace JobType = "fetch_stacktrace"
	JobResolveCheck    JobType = "resolve_check"
)

// Job represents a unit of work for the sync engine.
type Job struct {
	Type     JobType
	IssueID  string // empty for sync_issues
	Priority int    // lower = higher priority
}

// jobHeap implements heap.Interface for priority ordering.
type jobHeap []Job

func (h jobHeap) Len() int           { return len(h) }
func (h jobHeap) Less(i, j int) bool { return h[i].Priority < h[j].Priority }
func (h jobHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *jobHeap) Push(x any)        { *h = append(*h, x.(Job)) }
func (h *jobHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}

// JobQueue is a thread-safe priority queue for sync jobs.
type JobQueue struct {
	mu sync.Mutex
	h  jobHeap
}

func NewJobQueue() *JobQueue {
	q := &JobQueue{}
	heap.Init(&q.h)
	return q
}

func (q *JobQueue) Push(j Job) {
	q.mu.Lock()
	defer q.mu.Unlock()
	heap.Push(&q.h, j)
}

// Pop returns the highest-priority job. Returns zero Job if empty.
func (q *JobQueue) Pop() Job {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.h.Len() == 0 {
		return Job{}
	}
	return heap.Pop(&q.h).(Job)
}

func (q *JobQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.h.Len()
}
