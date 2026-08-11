// Package marketdata loads market_data_tick events (daily OHLCV, emitted by
// cmd/market-data-watcher from Yahoo Finance) from a FatBaby eventstore into
// an in-memory per-ticker price series. It is the read model backing
// newssite's own SVG chart renderer — the whole point of this package is
// that the chart draws from data EINHORN_INDUSTRIAL already ingested and
// stores itself (replayable from the event log), not a live third-party
// fetch at render time.
package marketdata

import (
	"context"
	"encoding/json"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
)

// Bar is one day's OHLCV bar, matching cmd/market-data-watcher's OHLCVBar
// event payload shape.
type Bar struct {
	Ticker    string    `json:"ticker"`
	Date      string    `json:"date"`
	Timestamp time.Time `json:"timestamp"`
	Open      float64   `json:"open"`
	High      float64   `json:"high"`
	Low       float64   `json:"low"`
	Close     float64   `json:"close"`
	AdjClose  float64   `json:"adj_close"`
	Volume    int64     `json:"volume"`
	// MarketCap is close * shares-outstanding as of the watcher's fetch time
	// (see cmd/market-data-watcher's refreshSharesOutstanding). 0/absent on
	// older events emitted before this field existed, or when shares
	// outstanding wasn't known yet for that ticker.
	MarketCap int64 `json:"market_cap,omitempty"`
}

// Store holds an in-memory snapshot of daily bars, per ticker, oldest-first.
// Safe for concurrent reads; refreshed by Build/Tail from the event store.
type Store struct {
	mu       sync.RWMutex
	byTicker map[string][]Bar // oldest-first per ticker; sequence within a ticker/date is deduped, last-write-wins
	seq      uint64
}

// NewStore returns an empty Store.
func NewStore() *Store {
	return &Store{byTicker: make(map[string][]Bar)}
}

// SeriesFor returns up to the last n daily bars for ticker, oldest-first
// (chart-ready order), or nil if the ticker has no data yet.
func (s *Store) SeriesFor(ticker string, n int) []Bar {
	ticker = strings.ToUpper(strings.TrimSpace(ticker))
	s.mu.RLock()
	src := s.byTicker[ticker]
	s.mu.RUnlock()
	if len(src) == 0 {
		return nil
	}
	if n <= 0 || n >= len(src) {
		cp := make([]Bar, len(src))
		copy(cp, src)
		return cp
	}
	cp := make([]Bar, n)
	copy(cp, src[len(src)-n:])
	return cp
}

// HasData reports whether any bars are loaded for ticker at all.
func (s *Store) HasData(ticker string) bool {
	ticker = strings.ToUpper(strings.TrimSpace(ticker))
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.byTicker[ticker]) > 0
}

// LatestMarketCap returns the most recent bar's market cap for ticker.
// ok=false when the ticker has no bars yet, or its latest bar predates
// market-cap enrichment / had no shares-outstanding data at fetch time.
func (s *Store) LatestMarketCap(ticker string) (int64, bool) {
	ticker = strings.ToUpper(strings.TrimSpace(ticker))
	s.mu.RLock()
	defer s.mu.RUnlock()
	bars := s.byTicker[ticker]
	if len(bars) == 0 {
		return 0, false
	}
	mc := bars[len(bars)-1].MarketCap
	if mc <= 0 {
		return 0, false
	}
	return mc, true
}

// ingest applies one batch of records to the store, keyed by ticker+date
// (last-write-wins per day, so a re-ingested/corrected bar replaces the old
// one rather than duplicating), then re-sorts each affected ticker's series
// by date.
func (s *Store) ingest(recs []eventstore.Record) {
	if len(recs) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	byTickerDate := make(map[string]map[string]Bar)
	for t, bars := range s.byTicker {
		m := make(map[string]Bar, len(bars))
		for _, b := range bars {
			m[b.Date] = b
		}
		byTickerDate[t] = m
	}

	touched := make(map[string]bool)
	for _, rec := range recs {
		if rec.Sequence > s.seq {
			s.seq = rec.Sequence
		}
		if rec.Event.Type != "market_data_tick" {
			continue
		}
		var bar Bar
		if err := json.Unmarshal(rec.Event.Data, &bar); err != nil {
			continue
		}
		ticker := strings.ToUpper(strings.TrimSpace(bar.Ticker))
		if ticker == "" || bar.Date == "" {
			continue
		}
		if byTickerDate[ticker] == nil {
			byTickerDate[ticker] = make(map[string]Bar)
		}
		byTickerDate[ticker][bar.Date] = bar
		touched[ticker] = true
	}

	for t := range touched {
		m := byTickerDate[t]
		out := make([]Bar, 0, len(m))
		for _, b := range m {
			out = append(out, b)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Date < out[j].Date })
		s.byTicker[t] = out
	}
}

// Build does a full initial scan of store from sequence 1, populating idx.
func Build(ctx context.Context, es eventstore.EventStore, idx *Store, logger *log.Logger) error {
	const batchSize = 1000
	from := uint64(1)
	total := 0
	for {
		recs, err := es.ReadFrom(ctx, from, batchSize)
		if err != nil {
			return err
		}
		if len(recs) == 0 {
			break
		}
		idx.ingest(recs)
		total += len(recs)
		from = recs[len(recs)-1].Sequence + 1
		if len(recs) < batchSize {
			break
		}
	}
	if logger != nil {
		logger.Printf("marketdata: initial build loaded %d events, %d tickers", total, idx.tickerCount())
	}
	return nil
}

func (s *Store) tickerCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.byTicker)
}

// Tail polls store for new events every interval and applies them. Returns a
// channel that's closed when the tail loop stops (ctx cancelled).
func Tail(ctx context.Context, es eventstore.EventStore, idx *Store, interval time.Duration, logger *log.Logger) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				idx.mu.RLock()
				from := idx.seq + 1
				idx.mu.RUnlock()
				recs, err := es.ReadFrom(ctx, from, 1000)
				if err != nil {
					if logger != nil {
						logger.Printf("marketdata: tail read error: %v", err)
					}
					continue
				}
				if len(recs) > 0 {
					idx.ingest(recs)
				}
			}
		}
	}()
	return done
}
