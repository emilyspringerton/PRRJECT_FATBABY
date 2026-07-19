package signalindex

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/example/prrject-fatbaby/pkg/intelligence"
)

// TODO(scale): Keep this as the migration seam for a future SQLite-backed index when startup scans exceed acceptable latency.
// Index holds all signal_generated events keyed by ticker.
// It is safe for concurrent reads and serial writes from the tail goroutine.
type Index struct {
	mu        sync.RWMutex
	byTicker  map[string][]*SignalEntry
	latestSeq uint64
}

// SignalEntry is the denormalized, query-ready view of one signal_generated record.
type SignalEntry struct {
	Seq             uint64            `json:"seq"`
	Ticker          string            `json:"ticker"`
	AccessionNumber string            `json:"accession_number"`
	Form            string            `json:"form"`
	FilingDate      string            `json:"filing_date"`
	SignalType      string            `json:"signal_type"`
	Importance      int               `json:"importance"`
	Sentiment       float64           `json:"sentiment"`
	Summary         string            `json:"summary"`
	ImpactAnalysis  string            `json:"impact_analysis"`
	Timestamp       time.Time         `json:"timestamp"`
	AppendedAt      time.Time         `json:"appended_at"`
	RawMetadata     map[string]string `json:"raw_metadata,omitempty"`
}

// TickerSummary is one summary row for a known ticker.
type TickerSummary struct {
	Ticker       string    `json:"ticker"`
	SignalCount  int       `json:"signal_count"`
	LatestSignal time.Time `json:"latest_signal"`
	LatestType   string    `json:"latest_signal_type"`
}

// NewIndex builds a new empty signal index.
func NewIndex() *Index { return &Index{byTicker: make(map[string][]*SignalEntry)} }

// Ingest adds one eventstore.Record to the index.
// signal_generated_v2 events replace the prior stub signal for the same identity
// (partition key) so backfill rewrites are reflected without a full restart.
func (idx *Index) Ingest(rec eventstore.Record) error {
	isV2 := rec.Event.Type == "signal_generated_v2"
	if rec.Event.Type != "signal_generated" && !isV2 {
		return nil
	}
	var signal intelligence.Signal
	if err := json.Unmarshal(rec.Event.Data, &signal); err != nil {
		return fmt.Errorf("unmarshal %s data: %w", rec.Event.Type, err)
	}
	ticker := normalizeTicker(signal.Ticker)
	if ticker == "" {
		return nil
	}
	_, accession := splitPartitionKey(rec.Event.PartitionKey)
	entry := &SignalEntry{
		Seq:             rec.Sequence,
		Ticker:          ticker,
		AccessionNumber: accession,
		Form:            signal.RawMetadata["form"],
		FilingDate:      signal.RawMetadata["filing_date"],
		SignalType:      signal.SignalType,
		Importance:      signal.Importance,
		Sentiment:       signal.Sentiment,
		Summary:         signal.Summary,
		ImpactAnalysis:  signal.ImpactAnalysis,
		Timestamp:       signal.Timestamp,
		AppendedAt:      rec.AppendedAt,
		RawMetadata:     signal.RawMetadata,
	}
	idx.mu.Lock()
	defer idx.mu.Unlock()
	items := idx.byTicker[ticker]
	if isV2 {
		// Replace the stub entry for the same accession number, if present.
		for i, it := range items {
			if it.AccessionNumber == accession {
				items[i] = entry
				idx.byTicker[ticker] = items
				if rec.Sequence > idx.latestSeq {
					idx.latestSeq = rec.Sequence
				}
				return nil
			}
		}
	}
	insertAt := sort.Search(len(items), func(i int) bool { return !items[i].Timestamp.Before(entry.Timestamp) })
	items = append(items, nil)
	copy(items[insertAt+1:], items[insertAt:])
	items[insertAt] = entry
	idx.byTicker[ticker] = items
	if rec.Sequence > idx.latestSeq {
		idx.latestSeq = rec.Sequence
	}
	return nil
}

// ForTicker returns a copy of all entries for a ticker, newest first.
func (idx *Index) ForTicker(ticker string) ([]*SignalEntry, bool) {
	ticker = normalizeTicker(ticker)
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	items, ok := idx.byTicker[ticker]
	if !ok || len(items) == 0 {
		return nil, false
	}
	out := make([]*SignalEntry, len(items))
	for i := range items {
		out[i] = items[len(items)-1-i]
	}
	return out, true
}

// Latest returns the single most recent entry for a ticker.
func (idx *Index) Latest(ticker string) (*SignalEntry, bool) {
	ticker = normalizeTicker(ticker)
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	items, ok := idx.byTicker[ticker]
	if !ok || len(items) == 0 {
		return nil, false
	}
	return items[len(items)-1], true
}

// Summary returns one row per known ticker: ticker, count, latest timestamp.
func (idx *Index) Summary() []TickerSummary {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	out := make([]TickerSummary, 0, len(idx.byTicker))
	for ticker, items := range idx.byTicker {
		if len(items) == 0 {
			continue
		}
		last := items[len(items)-1]
		out = append(out, TickerSummary{Ticker: ticker, SignalCount: len(items), LatestSignal: last.Timestamp, LatestType: last.SignalType})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SignalCount == out[j].SignalCount {
			return out[i].Ticker < out[j].Ticker
		}
		return out[i].SignalCount > out[j].SignalCount
	})
	return out
}

// TickerQuality holds per-ticker data quality metrics.
type TickerQuality struct {
	Ticker           string  `json:"ticker"`
	SignalCount       int     `json:"signal_count"`
	AvgConfidence     float64 `json:"avg_confidence"`
	MinConfidence     float64 `json:"min_confidence"`
	MaxConfidence     float64 `json:"max_confidence"`
	AvgImportance     float64 `json:"avg_importance"`
	GradeDistribution map[string]int `json:"grade_distribution"` // A/B/C/D/F → count
	FlagCounts        map[string]int `json:"flag_counts"`
	MedianLatencyMs   int64   `json:"median_latency_ms"` // appended_at - timestamp
}

// DataQuality computes quality metrics for every ticker in the index.
// sourceTypeForTicker is optional — pass nil to use empty source type for all signals.
func (idx *Index) DataQuality(sourceTypeForTicker func(ticker string) string) []TickerQuality {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	out := make([]TickerQuality, 0, len(idx.byTicker))
	for ticker, items := range idx.byTicker {
		if len(items) == 0 {
			continue
		}
		srcType := ""
		if sourceTypeForTicker != nil {
			srcType = sourceTypeForTicker(ticker)
		}

		tq := TickerQuality{
			Ticker:           ticker,
			SignalCount:       len(items),
			MinConfidence:     1.0,
			GradeDistribution: map[string]int{"A": 0, "B": 0, "C": 0, "D": 0, "F": 0},
			FlagCounts:        map[string]int{},
		}

		latencies := make([]int64, 0, len(items))
		sumConf := 0.0
		sumImp := 0.0
		for _, it := range items {
			sig := intelligence.Signal{
				Ticker:         it.Ticker,
				SignalType:     it.SignalType,
				Summary:        it.Summary,
				ImpactAnalysis: it.ImpactAnalysis,
				Importance:     it.Importance,
			}
			q := intelligence.ScoreSignal(sig, srcType)
			sumConf += q.Score
			sumImp += float64(it.Importance)
			if q.Score < tq.MinConfidence {
				tq.MinConfidence = q.Score
			}
			if q.Score > tq.MaxConfidence {
				tq.MaxConfidence = q.Score
			}
			grade := intelligence.Grade(q.Score)
			tq.GradeDistribution[grade]++
			for _, f := range q.Flags {
				tq.FlagCounts[f]++
			}
			if !it.AppendedAt.IsZero() && !it.Timestamp.IsZero() {
				latencies = append(latencies, it.AppendedAt.Sub(it.Timestamp).Milliseconds())
			}
		}
		tq.AvgConfidence = sumConf / float64(len(items))
		tq.AvgImportance = sumImp / float64(len(items))
		if len(latencies) > 0 {
			sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
			tq.MedianLatencyMs = latencies[len(latencies)/2]
		}
		out = append(out, tq)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].AvgConfidence != out[j].AvgConfidence {
			return out[i].AvgConfidence > out[j].AvgConfidence
		}
		return out[i].Ticker < out[j].Ticker
	})
	return out
}

// LatestSeq returns the highest sequence number ingested so far.
func (idx *Index) LatestSeq() uint64 { idx.mu.RLock(); defer idx.mu.RUnlock(); return idx.latestSeq }

// AllEntries returns a snapshot of every signal entry across all tickers, in
// no particular order. Used by internal/indexcheckpoint to persist the full
// index to a local SQLite checkpoint.
func (idx *Index) AllEntries() []*SignalEntry {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	out := make([]*SignalEntry, 0, len(idx.byTicker)*4)
	for _, items := range idx.byTicker {
		out = append(out, items...)
	}
	return out
}

// LoadEntries hydrates idx directly from previously-checkpointed entries
// (see internal/indexcheckpoint), bypassing event parsing. Entries are
// grouped by ticker and sorted oldest-first per ticker, matching Ingest's
// invariant (ForTicker relies on this order, reversing on read). Call on an
// empty, freshly-constructed Index before any Ingest calls.
func (idx *Index) LoadEntries(entries []*SignalEntry) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	for _, e := range entries {
		idx.byTicker[e.Ticker] = append(idx.byTicker[e.Ticker], e)
		if e.Seq > idx.latestSeq {
			idx.latestSeq = e.Seq
		}
	}
	for ticker, items := range idx.byTicker {
		sort.Slice(items, func(i, j int) bool { return items[i].Timestamp.Before(items[j].Timestamp) })
		idx.byTicker[ticker] = items
	}
}

// Depth returns total signal count across all tickers.
func (idx *Index) Depth() int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	total := 0
	for _, items := range idx.byTicker {
		total += len(items)
	}
	return total
}

func normalizeTicker(t string) string { return strings.ToUpper(strings.TrimSpace(t)) }

func splitPartitionKey(key string) (string, string) {
	parts := strings.SplitN(strings.TrimSpace(key), ":", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
}
