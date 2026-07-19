package signalindex

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/example/prrject-fatbaby/pkg/intelligence"
)

func sigRec(seq uint64, ticker string, ts time.Time) eventstore.Record {
	s := intelligence.Signal{ID: fmt.Sprintf("s-%d", seq), Ticker: ticker, Timestamp: ts, SignalType: "Earnings", Importance: int(seq), RawMetadata: map[string]string{"form": "8-K", "filing_date": "2026-01-01"}}
	b, _ := json.Marshal(s)
	ev := eventstore.Event{ID: fmt.Sprintf("e-%d", seq), Type: "signal_generated", OccurredAt: ts, PartitionKey: "1:acc", Data: b}
	return eventstore.Record{Sequence: seq, AppendedAt: ts.Add(time.Second), Event: ev}
}

func TestIngest_SignalGenerated(t *testing.T) {
	idx := NewIndex()
	_ = idx.Ingest(sigRec(1, "AAPL", time.Now()))
	got, ok := idx.ForTicker("AAPL")
	if !ok || len(got) != 1 {
		t.Fatal("expected one")
	}
}
func TestIngest_SkipsOtherTypes(t *testing.T) {
	idx := NewIndex()
	_ = idx.Ingest(eventstore.Record{Event: eventstore.Event{Type: "filing_discovered", Data: []byte(`{"a":1}`)}})
	if idx.Depth() != 0 {
		t.Fatal("expected 0")
	}
}
func TestIngest_SortOrder(t *testing.T) {
	idx := NewIndex()
	now := time.Now()
	_ = idx.Ingest(sigRec(1, "AAPL", now.Add(2*time.Hour)))
	_ = idx.Ingest(sigRec(2, "AAPL", now))
	_ = idx.Ingest(sigRec(3, "AAPL", now.Add(time.Hour)))
	idx.mu.RLock()
	items := idx.byTicker["AAPL"]
	idx.mu.RUnlock()
	if !(items[0].Timestamp.Before(items[1].Timestamp) && items[1].Timestamp.Before(items[2].Timestamp)) {
		t.Fatal("ascending")
	}
	latest, _ := idx.Latest("AAPL")
	if !latest.Timestamp.Equal(now.Add(2 * time.Hour)) {
		t.Fatal("latest")
	}
}
func TestForTicker_CaseInsensitive(t *testing.T) {
	idx := NewIndex()
	_ = idx.Ingest(sigRec(1, "AAPL", time.Now()))
	_, ok := idx.ForTicker("aapl")
	if !ok {
		t.Fatal("expected")
	}
}
func TestSummary_SortedByCount(t *testing.T) {
	idx := NewIndex()
	n := time.Now()
	_ = idx.Ingest(sigRec(1, "AAPL", n))
	_ = idx.Ingest(sigRec(2, "AAPL", n))
	_ = idx.Ingest(sigRec(3, "AAPL", n))
	_ = idx.Ingest(sigRec(4, "MSFT", n))
	s := idx.Summary()
	if s[0].Ticker != "AAPL" {
		t.Fatal("sorted")
	}
}
func TestBuild_ScansFullStore(t *testing.T) {
	store, _ := eventstore.NewFileStore(t.TempDir())
	defer store.Close()
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		r := sigRec(uint64(i+1), "AAPL", time.Now())
		if _, err := store.Append(ctx, r.Event); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 3; i++ {
		if _, err := store.Append(ctx, eventstore.Event{ID: fmt.Sprintf("x-%d", i), Type: "other", OccurredAt: time.Now(), Data: []byte(`{"x":1}`)}); err != nil {
			t.Fatal(err)
		}
	}
	idx := NewIndex()
	if err := Build(ctx, store, idx, 1, nil); err != nil {
		t.Fatal(err)
	}
	if idx.Depth() != 5 {
		t.Fatalf("got %d", idx.Depth())
	}
}
func TestTail_PicksUpNewRecords(t *testing.T) {
	store, _ := eventstore.NewFileStore(t.TempDir())
	defer store.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for i := 0; i < 5; i++ {
		r := sigRec(uint64(i+1), "AAPL", time.Now())
		if _, err := store.Append(ctx, r.Event); err != nil {
			t.Fatal(err)
		}
	}
	idx := NewIndex()
	_ = Build(ctx, store, idx, 1, nil)
	ready := Tail(ctx, store, idx, 50*time.Millisecond, nil)
	<-ready
	for i := 0; i < 2; i++ {
		r := sigRec(uint64(i+6), "AAPL", time.Now())
		if _, err := store.Append(ctx, r.Event); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if idx.Depth() == 7 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("depth %d", idx.Depth())
}

func TestAllEntries_ReturnsEverythingAcrossTickers(t *testing.T) {
	idx := NewIndex()
	_ = idx.Ingest(sigRec(1, "AAPL", time.Now()))
	_ = idx.Ingest(sigRec(2, "MSFT", time.Now()))
	_ = idx.Ingest(sigRec(3, "AAPL", time.Now()))

	all := idx.AllEntries()
	if len(all) != 3 {
		t.Fatalf("AllEntries = %d, want 3", len(all))
	}
}

func TestLoadEntries_HydratesAndSetsLatestSeq(t *testing.T) {
	idx := NewIndex()
	entries := []*SignalEntry{
		{Seq: 5, Ticker: "AAPL", AccessionNumber: "acc-1", Timestamp: time.Now().Add(-time.Hour)},
		{Seq: 9, Ticker: "AAPL", AccessionNumber: "acc-2", Timestamp: time.Now()},
		{Seq: 7, Ticker: "MSFT", AccessionNumber: "acc-3", Timestamp: time.Now()},
	}
	idx.LoadEntries(entries)

	if idx.LatestSeq() != 9 {
		t.Errorf("LatestSeq = %d, want 9 (max of loaded entries)", idx.LatestSeq())
	}
	if idx.Depth() != 3 {
		t.Errorf("Depth = %d, want 3", idx.Depth())
	}
	aapl, ok := idx.ForTicker("AAPL")
	if !ok || len(aapl) != 2 {
		t.Fatalf("ForTicker(AAPL) = %d entries, want 2", len(aapl))
	}
	// ForTicker returns newest-first; the seq-9 entry has the later Timestamp.
	if aapl[0].Seq != 9 {
		t.Errorf("expected newest-first ordering after LoadEntries, got seq=%d first", aapl[0].Seq)
	}
}

func TestLoadEntries_ThenIngestContinuesCorrectly(t *testing.T) {
	// Simulates the real startup path: hydrate from checkpoint, then resume
	// Build/Tail from the watermark -- new Ingest calls must merge cleanly
	// with pre-loaded data, not clobber it.
	idx := NewIndex()
	idx.LoadEntries([]*SignalEntry{
		{Seq: 5, Ticker: "AAPL", AccessionNumber: "acc-1", Timestamp: time.Now().Add(-time.Hour)},
	})
	_ = idx.Ingest(sigRec(6, "AAPL", time.Now()))

	aapl, ok := idx.ForTicker("AAPL")
	if !ok || len(aapl) != 2 {
		t.Fatalf("ForTicker(AAPL) = %d, want 2 (1 loaded + 1 ingested)", len(aapl))
	}
	if idx.LatestSeq() != 6 {
		t.Errorf("LatestSeq = %d, want 6", idx.LatestSeq())
	}
}
