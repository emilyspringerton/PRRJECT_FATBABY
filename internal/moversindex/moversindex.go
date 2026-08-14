// Package moversindex tails market_movers_snapshot events (written daily by
// cmd/movers-watcher) and maintains an in-memory per-ticker price-action
// history, following the same Build/Tail pattern as internal/signalindex and
// internal/newssite/docindex.
//
// movers-watcher has been recording these snapshots since 2026-07-28 but
// nothing read them back — see EMILY/BACKLOG.md SECTION 25's "Answer: yes,
// movers-watcher has been saving real structured price-action data" finding.
// This package is the read side: it surfaces the history that was already
// being collected, same class of gap as S154 (directors/earnings-date).
package moversindex

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
)

// Entry is one ticker's appearance in one day's gainers/losers snapshot.
type Entry struct {
	Date          string  `json:"date"` // YYYY-MM-DD, from the snapshot's FetchedAt
	Direction     string  `json:"direction"` // "gainer" or "loser"
	Price         float64 `json:"price"`
	Change        float64 `json:"change"`
	ChangePercent float64 `json:"change_percent"`
	Volume        int64   `json:"volume"`
	MarketCap     int64   `json:"market_cap"`
}

// snapshotQuote mirrors internal/movers.Quote's JSON shape without importing
// that package (same cmd/-boundary idiom cmd/movers-watcher itself uses for
// commentaryArticle) -- avoids a signalapi -> movers-watcher-only-concern
// import for a handful of fields.
type snapshotQuote struct {
	Symbol        string  `json:"symbol"`
	Price         float64 `json:"price"`
	Change        float64 `json:"change"`
	ChangePercent float64 `json:"change_percent"`
	Volume        int64   `json:"volume"`
	MarketCap     int64   `json:"market_cap"`
}

type snapshotPayload struct {
	FetchedAt time.Time       `json:"fetched_at"`
	Gainers   []snapshotQuote `json:"gainers"`
	Losers    []snapshotQuote `json:"losers"`
}

// Index holds movers history keyed by ticker, oldest-first.
// Safe for concurrent reads and serial writes from the tail goroutine.
type Index struct {
	mu        sync.RWMutex
	byTicker  map[string][]Entry
	seenDay   map[string]bool // dedup key: ticker+"|"+date, snapshot IDs are already per-day but this guards double-ingest defensively
	latestSeq uint64
}

// NewIndex returns an empty movers index.
func NewIndex() *Index {
	return &Index{
		byTicker: make(map[string][]Entry),
		seenDay:  make(map[string]bool),
	}
}

// Ingest processes one event store record, adding it to the index if it's a
// market_movers_snapshot. Safe to call with any event type; non-matching
// records are no-ops.
func (idx *Index) Ingest(rec eventstore.Record) error {
	if rec.Event.Type != "market_movers_snapshot" {
		return nil
	}
	var snap snapshotPayload
	if err := json.Unmarshal(rec.Event.Data, &snap); err != nil {
		return nil
	}
	date := snap.FetchedAt.Format("2006-01-02")

	idx.mu.Lock()
	defer idx.mu.Unlock()
	ingestOne := func(q snapshotQuote, direction string) {
		symbol := strings.ToUpper(strings.TrimSpace(q.Symbol))
		if symbol == "" {
			return
		}
		key := symbol + "|" + date + "|" + direction
		if idx.seenDay[key] {
			return
		}
		idx.seenDay[key] = true
		idx.byTicker[symbol] = append(idx.byTicker[symbol], Entry{
			Date:          date,
			Direction:     direction,
			Price:         q.Price,
			Change:        q.Change,
			ChangePercent: q.ChangePercent,
			Volume:        q.Volume,
			MarketCap:     q.MarketCap,
		})
	}
	for _, q := range snap.Gainers {
		ingestOne(q, "gainer")
	}
	for _, q := range snap.Losers {
		ingestOne(q, "loser")
	}
	if rec.Sequence > idx.latestSeq {
		idx.latestSeq = rec.Sequence
	}
	return nil
}

// LatestSeq returns the highest sequence number ingested so far.
func (idx *Index) LatestSeq() uint64 {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.latestSeq
}

// History returns a ticker's movers-snapshot history, newest first, capped
// at limit entries (limit<=0 means no cap).
func (idx *Index) History(ticker string, limit int) []Entry {
	ticker = strings.ToUpper(strings.TrimSpace(ticker))
	idx.mu.RLock()
	src := idx.byTicker[ticker]
	cp := make([]Entry, len(src))
	copy(cp, src)
	idx.mu.RUnlock()
	sort.Slice(cp, func(i, j int) bool { return cp[i].Date > cp[j].Date })
	if limit > 0 && len(cp) > limit {
		cp = cp[:limit]
	}
	return cp
}

// Build scans the store from fromSeq and populates idx. fromSeq is normally 1
// (full history) -- snapshot volume is small (one event/day), so a full scan
// on every restart is cheap; no checkpoint persistence needed at this scale.
func Build(ctx context.Context, store eventstore.EventStore, idx *Index, fromSeq uint64, logger *log.Logger) error {
	return store.Scan(ctx, fromSeq, func(rec eventstore.Record) error {
		if err := idx.Ingest(rec); err != nil && logger != nil {
			logger.Printf("moversindex ingest: %v", err)
		}
		return nil
	})
}

// Tail starts a background goroutine that polls the store for new records.
// It closes the returned channel once the first poll completes (signals readiness).
func Tail(ctx context.Context, store eventstore.EventStore, idx *Index, interval time.Duration, logger *log.Logger) <-chan struct{} {
	ready := make(chan struct{})
	poll := func() {
		start := idx.LatestSeq() + 1
		err := store.Scan(ctx, start, func(rec eventstore.Record) error {
			_ = idx.Ingest(rec)
			return nil
		})
		if err != nil && !errors.Is(err, context.Canceled) && logger != nil {
			logger.Printf("moversindex tail: %v", err)
		}
	}
	go func() {
		defer close(ready)
		if ctx.Err() != nil {
			return
		}
		poll()
		ready <- struct{}{}
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				poll()
			}
		}
	}()
	return ready
}
