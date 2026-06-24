package cooccurrence

import (
	"sort"
	"time"
)

// TickerSignal is a lightweight signal event for co-occurrence seeding.
type TickerSignal struct {
	Ticker    string
	Timestamp time.Time
}

// SeedFromSignals populates the store from a list of per-ticker signal events.
// For each 48h window, any two tickers that both have a signal are paired.
// This is a best-effort startup warm-up; it does not affect live recording.
func (s *Store) SeedFromSignals(signals []TickerSignal) {
	if len(signals) == 0 {
		return
	}
	// Sort by timestamp ascending.
	sort.Slice(signals, func(i, j int) bool {
		return signals[i].Timestamp.Before(signals[j].Timestamp)
	})
	window := time.Duration(WindowHours) * time.Hour
	for i, a := range signals {
		for j := i + 1; j < len(signals); j++ {
			b := signals[j]
			if b.Timestamp.Sub(a.Timestamp) > window {
				break
			}
			if a.Ticker == b.Ticker {
				continue
			}
			// Use the midpoint as the co-occurrence timestamp.
			mid := a.Timestamp.Add(b.Timestamp.Sub(a.Timestamp) / 2)
			s.Record(a.Ticker, b.Ticker, mid)
		}
	}
}
