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

// LatestSeq returns the highest sequence number ingested so far.
func (idx *Index) LatestSeq() uint64 { idx.mu.RLock(); defer idx.mu.RUnlock(); return idx.latestSeq }

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
