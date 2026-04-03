package reports

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTimeSeries_WriteAndRead(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)

	issueID := "test-issue-1"
	os.MkdirAll(filepath.Join(dir, issueID), 0755)

	ts := &TimeSeries{
		Buckets: []Bucket{
			{Timestamp: "2026-04-03T10:00:00Z", Count: 12},
			{Timestamp: "2026-04-03T11:00:00Z", Count: 8},
		},
		Window:      "24h",
		LastFetched: time.Date(2026, 4, 3, 14, 0, 0, 0, time.UTC).Format(time.RFC3339),
	}

	if err := mgr.WriteTimeSeries(issueID, ts); err != nil {
		t.Fatalf("WriteTimeSeries: %v", err)
	}

	got, err := mgr.ReadTimeSeries(issueID)
	if err != nil {
		t.Fatalf("ReadTimeSeries: %v", err)
	}

	if len(got.Buckets) != 2 {
		t.Fatalf("expected 2 buckets, got %d", len(got.Buckets))
	}
	if got.Buckets[0].Count != 12 {
		t.Errorf("expected first bucket count 12, got %d", got.Buckets[0].Count)
	}
	if got.Window != "24h" {
		t.Errorf("expected window 24h, got %s", got.Window)
	}
}

func TestTimeSeries_ReadMissing(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)

	_, err := mgr.ReadTimeSeries("nonexistent")
	if err == nil {
		t.Error("expected error reading nonexistent timeseries")
	}
}

func TestTimeSeries_IsStale(t *testing.T) {
	fresh := &TimeSeries{
		Window:      "24h",
		LastFetched: time.Now().UTC().Format(time.RFC3339),
	}
	if fresh.IsStale("24h", 15*time.Minute) {
		t.Error("freshly fetched timeseries should not be stale")
	}

	stale := &TimeSeries{
		Window:      "24h",
		LastFetched: time.Now().Add(-20 * time.Minute).UTC().Format(time.RFC3339),
	}
	if !stale.IsStale("24h", 15*time.Minute) {
		t.Error("timeseries fetched 20m ago should be stale at 15m threshold")
	}

	wrongWindow := &TimeSeries{
		Window:      "1h",
		LastFetched: time.Now().UTC().Format(time.RFC3339),
	}
	if !wrongWindow.IsStale("24h", 15*time.Minute) {
		t.Error("timeseries with wrong window should be stale")
	}
}
