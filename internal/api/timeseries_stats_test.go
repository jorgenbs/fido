package api

import (
	"testing"

	"github.com/ruter-as/fido/internal/reports"
)

func makeBuckets(counts []int64) []reports.Bucket {
	buckets := make([]reports.Bucket, len(counts))
	for i, c := range counts {
		buckets[i] = reports.Bucket{Timestamp: "2024-01-01T00:00:00Z", Count: c}
	}
	return buckets
}

func TestComputeTimeseriesStats_Rising(t *testing.T) {
	// Second half (150+200+250+300=900) > first half (10+20+30+40=100) by >25%
	buckets := makeBuckets([]int64{10, 20, 30, 40, 150, 200, 250, 300})
	stats := computeTimeseriesStats(buckets)

	if stats.Total != 1000 {
		t.Errorf("expected Total=1000, got %d", stats.Total)
	}
	if stats.Peak != 300 {
		t.Errorf("expected Peak=300, got %d", stats.Peak)
	}
	if stats.Trend != "rising" {
		t.Errorf("expected Trend=rising, got %s", stats.Trend)
	}
}

func TestComputeTimeseriesStats_Declining(t *testing.T) {
	// First half (300+250+200+150=900) > second half (40+30+20+10=100)
	buckets := makeBuckets([]int64{300, 250, 200, 150, 40, 30, 20, 10})
	stats := computeTimeseriesStats(buckets)

	if stats.Total != 1000 {
		t.Errorf("expected Total=1000, got %d", stats.Total)
	}
	if stats.Peak != 300 {
		t.Errorf("expected Peak=300, got %d", stats.Peak)
	}
	if stats.Trend != "declining" {
		t.Errorf("expected Trend=declining, got %s", stats.Trend)
	}
}

func TestComputeTimeseriesStats_Stable(t *testing.T) {
	// Both halves equal: 100+100+100+100
	buckets := makeBuckets([]int64{100, 100, 100, 100})
	stats := computeTimeseriesStats(buckets)

	if stats.Total != 400 {
		t.Errorf("expected Total=400, got %d", stats.Total)
	}
	if stats.Peak != 100 {
		t.Errorf("expected Peak=100, got %d", stats.Peak)
	}
	if stats.Trend != "stable" {
		t.Errorf("expected Trend=stable, got %s", stats.Trend)
	}
}

func TestComputeTimeseriesStats_Empty(t *testing.T) {
	stats := computeTimeseriesStats(nil)

	if stats.Total != 0 {
		t.Errorf("expected Total=0, got %d", stats.Total)
	}
	if stats.Peak != 0 {
		t.Errorf("expected Peak=0, got %d", stats.Peak)
	}
	if stats.Trend != "stable" {
		t.Errorf("expected Trend=stable, got %s", stats.Trend)
	}
}

func TestComputeTimeseriesStats_SingleBucket(t *testing.T) {
	buckets := makeBuckets([]int64{42})
	stats := computeTimeseriesStats(buckets)

	if stats.Total != 42 {
		t.Errorf("expected Total=42, got %d", stats.Total)
	}
	if stats.Peak != 42 {
		t.Errorf("expected Peak=42, got %d", stats.Peak)
	}
	if stats.Trend != "stable" {
		t.Errorf("expected Trend=stable, got %s", stats.Trend)
	}
}
