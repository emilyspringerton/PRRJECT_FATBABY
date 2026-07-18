// Package docindex tails source_document_persisted events and maintains an
// in-memory per-ticker document summary, following the same Build/Tail pattern
// as internal/signalindex.
package docindex

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/example/prrject-fatbaby/pkg/intelligence"
)

// DocSummary is the per-document record kept in the in-memory index.
// It carries all fields needed by the front page, ticker page, wire, and archive
// so those handlers never have to touch the raw event store on a hot path.
type DocSummary struct {
	Identity    string
	Ticker      string
	SourceType  string
	Form        string
	DocumentURL string
	BodyPreview string // first ~800 runes of cleaned text
	CharCount   int
	FilingDate  string    // SEC filing date (YYYY-MM-DD); empty for legacy docs
	PersistedAt time.Time // pipeline-index timestamp
	Sequence    uint64    // event store sequence; enables targeted fetch for detail pages
}

// Index holds all doc summaries keyed by ticker and by identity.
// Safe for concurrent reads and serial writes from the tail goroutine.
type Index struct {
	mu         sync.RWMutex
	byTicker   map[string][]*DocSummary // sorted oldest-first; reverse on read
	byIdentity map[string]*DocSummary
	latestSeq  uint64
}

// NewIndex returns an empty doc index.
func NewIndex() *Index {
	return &Index{
		byTicker:   make(map[string][]*DocSummary),
		byIdentity: make(map[string]*DocSummary),
	}
}

// Ingest adds one eventstore.Record to the index. Non-doc events are skipped.
func (idx *Index) Ingest(rec eventstore.Record) error {
	if rec.Event.Type != "source_document_persisted" {
		return nil
	}
	var doc intelligence.SourceDocument
	if err := json.Unmarshal(rec.Event.Data, &doc); err != nil {
		return nil
	}
	ticker := strings.ToUpper(strings.TrimSpace(doc.Ticker))
	if ticker == "" || doc.Identity == "" {
		return nil
	}
	idx.mu.Lock()
	defer idx.mu.Unlock()
	if _, exists := idx.byIdentity[doc.Identity]; exists {
		return nil
	}
	ds := &DocSummary{
		Identity:    doc.Identity,
		Ticker:      ticker,
		SourceType:  doc.SourceType,
		Form:        doc.Form,
		DocumentURL: doc.DocumentURL,
		BodyPreview: previewText(doc.CleanedText, 800),
		CharCount:   doc.CleanedCharCount,
		FilingDate:  doc.FilingDate,
		PersistedAt: doc.PersistedAt,
		Sequence:    rec.Sequence,
	}
	idx.byIdentity[doc.Identity] = ds
	idx.byTicker[ticker] = append(idx.byTicker[ticker], ds)
	if rec.Sequence > idx.latestSeq {
		idx.latestSeq = rec.Sequence
	}
	return nil
}

// ForTicker returns all docs for a ticker, newest first by FilingDate.
// Allocation on each call is intentional (cheap copy; list is short; avoids exposing the internal slice).
// ByIdentity returns the summary for one document by its identity, in O(1)
// via the in-memory index — the fast path for detail-page lookups. Callers
// needing full body text should use the returned summary's Sequence for a
// targeted eventstore.ReadFrom(ctx, seq, 1) fetch, not a full-store scan.
func (idx *Index) ByIdentity(identity string) (*DocSummary, bool) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	ds, ok := idx.byIdentity[identity]
	return ds, ok
}

func (idx *Index) ForTicker(ticker string) []*DocSummary {
	ticker = strings.ToUpper(strings.TrimSpace(ticker))
	idx.mu.RLock()
	src := idx.byTicker[ticker]
	cp := make([]*DocSummary, len(src))
	copy(cp, src)
	idx.mu.RUnlock()
	sort.Slice(cp, func(i, j int) bool {
		return docNewerThan(cp[i], cp[j])
	})
	return cp
}

// AllSummaries returns a snapshot of every doc, newest first.
// Sort key: FilingDate descending (SEC filing date), falling back to
// PersistedAt for docs where FilingDate is absent.
func (idx *Index) AllSummaries() []*DocSummary {
	idx.mu.RLock()
	out := make([]*DocSummary, 0, len(idx.byIdentity))
	for _, ds := range idx.byIdentity {
		out = append(out, ds)
	}
	idx.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		return docNewerThan(out[i], out[j])
	})
	return out
}

// docNewerThan returns true when a was filed more recently than b.
// Compares FilingDate first (lexicographic YYYY-MM-DD comparison is correct),
// falling back to PersistedAt when FilingDate is absent.
func docNewerThan(a, b *DocSummary) bool {
	fa, fb := a.FilingDate, b.FilingDate
	switch {
	case fa != "" && fb != "":
		return fa > fb
	case fa != "":
		return true // dated > undated
	case fb != "":
		return false
	default:
		return a.PersistedAt.After(b.PersistedAt)
	}
}

// Recent returns up to n docs across all tickers, newest-first by PersistedAt.
// All fields including BodyPreview and FilingDate are populated from the index;
// no event store I/O is performed.
func (idx *Index) Recent(n int) []*DocSummary {
	all := idx.AllSummaries()
	if len(all) > n {
		all = all[:n]
	}
	return all
}

// KnownTickers returns a sorted list of all indexed ticker symbols.
func (idx *Index) KnownTickers() []string {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	tickers := make([]string, 0, len(idx.byTicker))
	for t := range idx.byTicker {
		tickers = append(tickers, t)
	}
	sort.Strings(tickers)
	return tickers
}

// LatestSeq returns the highest sequence number ingested so far.
func (idx *Index) LatestSeq() uint64 {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.latestSeq
}

// Build scans the store from fromSeq and populates idx. fromSeq is normally 1
// (full history); pass a higher value only via the documented
// -replay-from-seq emergency degraded-mode lever.
func Build(ctx context.Context, store eventstore.EventStore, idx *Index, fromSeq uint64, logger *log.Logger) error {
	return store.Scan(ctx, fromSeq, func(rec eventstore.Record) error {
		if err := idx.Ingest(rec); err != nil && logger != nil {
			logger.Printf("docindex ingest: %v", err)
		}
		return nil
	})
}

// Tail starts a background goroutine that polls the store for new records.
// It closes the returned channel once the first poll completes (signals readiness).
// The first poll runs immediately rather than waiting a full interval, since
// Build() has typically just caught the index up to latest_seq and this poll
// is normally a fast no-op.
func Tail(ctx context.Context, store eventstore.EventStore, idx *Index, interval time.Duration, logger *log.Logger) <-chan struct{} {
	ready := make(chan struct{})
	poll := func() {
		start := idx.LatestSeq() + 1
		err := store.Scan(ctx, start, func(rec eventstore.Record) error {
			_ = idx.Ingest(rec)
			return nil
		})
		if err != nil && !errors.Is(err, context.Canceled) && logger != nil {
			logger.Printf("docindex tail: %v", err)
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

func previewText(s string, maxRunes int) string {
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	runes := []rune(s)
	trimmed := string(runes[:maxRunes])
	if i := strings.LastIndex(trimmed, " "); i > 0 {
		return strings.TrimSpace(trimmed[:i])
	}
	return strings.TrimSpace(trimmed)
}
