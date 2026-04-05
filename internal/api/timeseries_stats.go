package api

import "github.com/ruter-as/fido/internal/reports"

// timeseriesStats holds computed aggregate statistics for a bucket slice.
type timeseriesStats struct {
	Total int64  `json:"total"`
	Peak  int64  `json:"peak"`
	Trend string `json:"trend"`
}

// computeTimeseriesStats computes total, peak, and trend from a slice of buckets.
// Trend is determined by comparing the sum of the second half to the first half:
//   - "rising"   if second/first ratio > 1.25
//   - "declining" if second/first ratio < 0.75
//   - "stable"   otherwise, or when < 4 buckets are available
func computeTimeseriesStats(buckets []reports.Bucket) timeseriesStats {
	if len(buckets) == 0 {
		return timeseriesStats{Trend: "stable"}
	}

	var total, peak int64
	for _, b := range buckets {
		total += b.Count
		if b.Count > peak {
			peak = b.Count
		}
	}

	trend := "stable"
	if len(buckets) >= 4 {
		mid := len(buckets) / 2
		var firstSum, secondSum int64
		for _, b := range buckets[:mid] {
			firstSum += b.Count
		}
		for _, b := range buckets[mid:] {
			secondSum += b.Count
		}
		if firstSum > 0 {
			ratio := float64(secondSum) / float64(firstSum)
			if ratio > 1.25 {
				trend = "rising"
			} else if ratio < 0.75 {
				trend = "declining"
			}
		} else if secondSum > 0 {
			// first half is zero but second has activity — rising
			trend = "rising"
		}
	}

	return timeseriesStats{Total: total, Peak: peak, Trend: trend}
}
