package reports

import (
	"encoding/json"
	"fmt"
	"time"
)

// Bucket is a single time-bucketed occurrence count.
type Bucket struct {
	Timestamp string `json:"timestamp"`
	Count     int64  `json:"count"`
}

// TimeSeries holds cached occurrence data for an issue.
type TimeSeries struct {
	Buckets     []Bucket `json:"buckets"`
	Window      string   `json:"window"`
	LastFetched string   `json:"last_fetched"`
}

// IsStale returns true if the cached data doesn't cover the requested window
// or was fetched longer ago than maxAge.
func (ts *TimeSeries) IsStale(window string, maxAge time.Duration) bool {
	if ts.Window != window {
		return true
	}
	fetched, err := time.Parse(time.RFC3339, ts.LastFetched)
	if err != nil {
		return true
	}
	return time.Since(fetched) > maxAge
}

func (m *Manager) WriteTimeSeries(issueID string, ts *TimeSeries) error {
	b, err := json.MarshalIndent(ts, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling timeseries: %w", err)
	}
	return m.writeFile(issueID, "timeseries.json", string(b))
}

func (m *Manager) ReadTimeSeries(issueID string) (*TimeSeries, error) {
	content, err := m.readFile(issueID, "timeseries.json")
	if err != nil {
		return nil, err
	}
	var ts TimeSeries
	if err := json.Unmarshal([]byte(content), &ts); err != nil {
		return nil, fmt.Errorf("parsing timeseries.json: %w", err)
	}
	return &ts, nil
}
