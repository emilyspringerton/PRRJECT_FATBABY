package signalindex

import "time"

// VelocityAlert is issued when a ticker produces too many high-confidence signals in a short window.
type VelocityAlert struct {
	Ticker      string
	Count       int
	WindowStart time.Time
	WindowEnd   time.Time
	Signals     []*SignalEntry
}

// VelocityThreshold defines the parameters for a velocity check.
type VelocityThreshold struct {
	// MinCount is the minimum number of signals required to trigger an alert (exclusive, i.e. >MinCount).
	MinCount int
	// Window is the time window to search (e.g. 1 hour).
	Window time.Duration
	// MinImportance is the minimum signal importance (0-100) to count.
	// Signals with Importance < MinImportance are ignored.
	MinImportance int
}

// DefaultVelocityThreshold mirrors the S126-12 spec: >3 signals in <1h with importance>60.
var DefaultVelocityThreshold = VelocityThreshold{
	MinCount:      3,
	Window:        time.Hour,
	MinImportance: 60,
}

// CheckVelocity scans the index for tickers that exceed the threshold as of `now`.
// Returns one VelocityAlert per triggering ticker. Tickers with no qualifying signals are skipped.
func (idx *Index) CheckVelocity(now time.Time, t VelocityThreshold) []VelocityAlert {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	windowStart := now.Add(-t.Window)
	var alerts []VelocityAlert

	for ticker, entries := range idx.byTicker {
		var matches []*SignalEntry
		for _, e := range entries {
			if e.Timestamp.Before(windowStart) {
				continue
			}
			if e.Importance < t.MinImportance {
				continue
			}
			matches = append(matches, e)
		}
		if len(matches) > t.MinCount {
			// Find actual window bounds.
			start, end := matches[0].Timestamp, matches[0].Timestamp
			for _, m := range matches[1:] {
				if m.Timestamp.Before(start) {
					start = m.Timestamp
				}
				if m.Timestamp.After(end) {
					end = m.Timestamp
				}
			}
			alerts = append(alerts, VelocityAlert{
				Ticker:      ticker,
				Count:       len(matches),
				WindowStart: start,
				WindowEnd:   end,
				Signals:     matches,
			})
		}
	}
	return alerts
}
