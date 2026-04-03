// internal/syncer/queue_test.go
package syncer

import "testing"

func TestQueue_PriorityOrdering(t *testing.T) {
	q := NewJobQueue()

	q.Push(Job{Type: JobFetchBuckets, IssueID: "b", Priority: 2})
	q.Push(Job{Type: JobSyncIssues, Priority: 0})
	q.Push(Job{Type: JobFetchStacktrace, IssueID: "c", Priority: 3})

	first := q.Pop()
	if first.Type != JobSyncIssues {
		t.Errorf("expected sync_issues first (priority 0), got %s", first.Type)
	}

	second := q.Pop()
	if second.Type != JobFetchBuckets {
		t.Errorf("expected fetch_buckets second (priority 2), got %s", second.Type)
	}

	third := q.Pop()
	if third.Type != JobFetchStacktrace {
		t.Errorf("expected fetch_stacktrace third (priority 3), got %s", third.Type)
	}
}

func TestQueue_EmptyReturnsZero(t *testing.T) {
	q := NewJobQueue()
	j := q.Pop()
	if j.Type != "" {
		t.Errorf("expected zero job from empty queue, got %s", j.Type)
	}
}

func TestQueue_Len(t *testing.T) {
	q := NewJobQueue()
	q.Push(Job{Type: JobSyncIssues})
	q.Push(Job{Type: JobFetchBuckets, IssueID: "a"})

	if q.Len() != 2 {
		t.Errorf("expected len 2, got %d", q.Len())
	}

	q.Pop()
	if q.Len() != 1 {
		t.Errorf("expected len 1, got %d", q.Len())
	}
}
