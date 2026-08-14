package moversindex

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
)

func snapRec(seq uint64, fetchedAt time.Time, gainers, losers []snapshotQuote) eventstore.Record {
	snap := snapshotPayload{FetchedAt: fetchedAt, Gainers: gainers, Losers: losers}
	b, _ := json.Marshal(snap)
	return eventstore.Record{
		Sequence:   seq,
		AppendedAt: fetchedAt,
		Event: eventstore.Event{
			ID:   "market_movers_snapshot:test",
			Type: "market_movers_snapshot",
			Data: b,
		},
	}
}

func TestIngest_SkipsNonSnapshotEvents(t *testing.T) {
	idx := NewIndex()
	rec := eventstore.Record{Event: eventstore.Event{Type: "filing_discovered", Data: []byte(`{}`)}}
	if err := idx.Ingest(rec); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := idx.History("AAPL", 0); len(got) != 0 {
		t.Errorf("expected empty history, got %v", got)
	}
}

func TestIngest_StoresGainersAndLosers(t *testing.T) {
	idx := NewIndex()
	day := time.Date(2026, 8, 1, 14, 30, 0, 0, time.UTC)
	rec := snapRec(1, day,
		[]snapshotQuote{{Symbol: "aapl", Price: 200, Change: 5, ChangePercent: 2.5, Volume: 1000, MarketCap: 3_000_000_000}},
		[]snapshotQuote{{Symbol: "tsla", Price: 180, Change: -10, ChangePercent: -5.2, Volume: 2000, MarketCap: 500_000_000}},
	)
	if err := idx.Ingest(rec); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	aapl := idx.History("AAPL", 0)
	if len(aapl) != 1 || aapl[0].Direction != "gainer" || aapl[0].Date != "2026-08-01" {
		t.Fatalf("unexpected AAPL history: %+v", aapl)
	}
	tsla := idx.History("tsla", 0) // lowercase input, index should normalize
	if len(tsla) != 1 || tsla[0].Direction != "loser" {
		t.Fatalf("unexpected TSLA history: %+v", tsla)
	}
}

func TestIngest_DeduplicatesSameTickerSameDaySameDirection(t *testing.T) {
	idx := NewIndex()
	day := time.Date(2026, 8, 1, 14, 30, 0, 0, time.UTC)
	rec := snapRec(1, day, []snapshotQuote{{Symbol: "AAPL", Price: 200}}, nil)
	if err := idx.Ingest(rec); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Re-ingesting the identical record (e.g. a Tail() re-poll boundary overlap)
	// must not double the history.
	if err := idx.Ingest(rec); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := idx.History("AAPL", 0); len(got) != 1 {
		t.Fatalf("expected 1 deduplicated entry, got %d: %+v", len(got), got)
	}
}

func TestHistory_NewestFirstAndLimit(t *testing.T) {
	idx := NewIndex()
	days := []string{"2026-07-28", "2026-07-29", "2026-07-30"}
	for i, d := range days {
		day, _ := time.Parse("2006-01-02", d)
		rec := snapRec(uint64(i+1), day, []snapshotQuote{{Symbol: "AAPL", Price: float64(100 + i)}}, nil)
		if err := idx.Ingest(rec); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	all := idx.History("AAPL", 0)
	if len(all) != 3 || all[0].Date != "2026-07-30" || all[2].Date != "2026-07-28" {
		t.Fatalf("expected newest-first ordering, got %+v", all)
	}
	limited := idx.History("AAPL", 2)
	if len(limited) != 2 || limited[0].Date != "2026-07-30" {
		t.Fatalf("expected limit=2 newest-first, got %+v", limited)
	}
}

func TestIngest_SkipsEmptySymbol(t *testing.T) {
	idx := NewIndex()
	day := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	rec := snapRec(1, day, []snapshotQuote{{Symbol: "  ", Price: 1}}, nil)
	if err := idx.Ingest(rec); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := idx.History("", 0); len(got) != 0 {
		t.Errorf("expected no entry indexed under empty ticker, got %v", got)
	}
}

func TestLatestSeq_TracksHighestIngestedSequence(t *testing.T) {
	idx := NewIndex()
	day := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	_ = idx.Ingest(snapRec(5, day, []snapshotQuote{{Symbol: "AAPL"}}, nil))
	_ = idx.Ingest(snapRec(3, day, []snapshotQuote{{Symbol: "TSLA"}}, nil))
	if got := idx.LatestSeq(); got != 5 {
		t.Errorf("expected latestSeq=5, got %d", got)
	}
}
