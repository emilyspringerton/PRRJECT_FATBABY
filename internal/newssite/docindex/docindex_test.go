package docindex

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/example/prrject-fatbaby/pkg/intelligence"
)

func docRec(seq uint64, ticker, identity string, filingDate string, persistedAt time.Time) eventstore.Record {
	doc := intelligence.SourceDocument{
		Identity:         identity,
		Ticker:           ticker,
		SourceType:       "sec_8k",
		Form:             "8-K",
		CleanedText:      "Some cleaned text for the document body.",
		CleanedCharCount: 41,
		FilingDate:       filingDate,
		PersistedAt:      persistedAt,
	}
	b, _ := json.Marshal(doc)
	ev := eventstore.Event{
		ID:   identity,
		Type: "source_document_persisted",
		Data: b,
	}
	return eventstore.Record{Sequence: seq, AppendedAt: persistedAt, Event: ev}
}

func TestIngest_SkipsNonDocEvents(t *testing.T) {
	idx := NewIndex()
	rec := eventstore.Record{Event: eventstore.Event{Type: "filing_discovered", Data: []byte(`{}`)}}
	if err := idx.Ingest(rec); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := idx.KnownTickers(); len(got) != 0 {
		t.Errorf("expected empty index, got tickers: %v", got)
	}
}

func TestIngest_StoresDocument(t *testing.T) {
	idx := NewIndex()
	now := time.Now().UTC()
	if err := idx.Ingest(docRec(1, "AAPL", "id1", "2019-04-26", now)); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	docs := idx.ForTicker("AAPL")
	if len(docs) != 1 {
		t.Fatalf("ForTicker returned %d docs, want 1", len(docs))
	}
	d := docs[0]
	if d.Identity != "id1" {
		t.Errorf("Identity = %q, want %q", d.Identity, "id1")
	}
	if d.FilingDate != "2019-04-26" {
		t.Errorf("FilingDate = %q, want 2019-04-26", d.FilingDate)
	}
	if d.Ticker != "AAPL" {
		t.Errorf("Ticker = %q, want AAPL", d.Ticker)
	}
}

func TestIngest_DeduplicatesByIdentity(t *testing.T) {
	idx := NewIndex()
	now := time.Now().UTC()
	_ = idx.Ingest(docRec(1, "AAPL", "dup-id", "2019-04-26", now))
	_ = idx.Ingest(docRec(2, "AAPL", "dup-id", "2019-04-26", now.Add(time.Second)))
	if got := idx.ForTicker("AAPL"); len(got) != 1 {
		t.Errorf("ForTicker returned %d docs after duplicate ingest, want 1", len(got))
	}
}

func TestIngest_NormalizesTickerToUpper(t *testing.T) {
	idx := NewIndex()
	now := time.Now().UTC()
	doc := intelligence.SourceDocument{Identity: "x1", Ticker: "aapl", SourceType: "sec_8k", PersistedAt: now}
	b, _ := json.Marshal(doc)
	_ = idx.Ingest(eventstore.Record{Sequence: 1, Event: eventstore.Event{Type: "source_document_persisted", Data: b}})
	if docs := idx.ForTicker("AAPL"); len(docs) != 1 {
		t.Errorf("ForTicker(AAPL) returned %d docs after lowercase ingest, want 1", len(docs))
	}
}

func TestIngest_SkipsEmptyIdentity(t *testing.T) {
	idx := NewIndex()
	now := time.Now().UTC()
	doc := intelligence.SourceDocument{Identity: "", Ticker: "AAPL", SourceType: "sec_8k", PersistedAt: now}
	b, _ := json.Marshal(doc)
	_ = idx.Ingest(eventstore.Record{Sequence: 1, Event: eventstore.Event{Type: "source_document_persisted", Data: b}})
	if docs := idx.ForTicker("AAPL"); len(docs) != 0 {
		t.Errorf("expected no docs for empty identity, got %d", len(docs))
	}
}

func TestForTicker_NewestFirst(t *testing.T) {
	idx := NewIndex()
	base := time.Now().UTC()
	_ = idx.Ingest(docRec(1, "SCHW", "oldest", "2021-01-01", base))
	_ = idx.Ingest(docRec(2, "SCHW", "middle", "2022-06-15", base.Add(time.Hour)))
	_ = idx.Ingest(docRec(3, "SCHW", "newest", "2023-03-10", base.Add(2*time.Hour)))

	docs := idx.ForTicker("SCHW")
	if len(docs) != 3 {
		t.Fatalf("got %d docs, want 3", len(docs))
	}
	if docs[0].Identity != "newest" || docs[1].Identity != "middle" || docs[2].Identity != "oldest" {
		t.Errorf("order wrong: got %q %q %q", docs[0].Identity, docs[1].Identity, docs[2].Identity)
	}
}

func TestForTicker_UnknownReturnsEmpty(t *testing.T) {
	idx := NewIndex()
	if docs := idx.ForTicker("ZZZZ"); len(docs) != 0 {
		t.Errorf("unknown ticker returned %d docs, want 0", len(docs))
	}
}

func TestRecent_LimitsAndSortsNewestFirst(t *testing.T) {
	idx := NewIndex()
	base := time.Now().UTC()
	for i := 0; i < 5; i++ {
		_ = idx.Ingest(docRec(uint64(i+1), "AAPL", string(rune('a'+i)), "",
			base.Add(time.Duration(i)*time.Hour)))
	}
	got := idx.Recent(3)
	if len(got) != 3 {
		t.Fatalf("Recent(3) = %d items, want 3", len(got))
	}
	for i := 1; i < len(got); i++ {
		if !got[i-1].PersistedAt.After(got[i].PersistedAt) {
			t.Errorf("not sorted newest-first at index %d", i)
		}
	}
}

func TestRecent_CapAtCount(t *testing.T) {
	idx := NewIndex()
	base := time.Now().UTC()
	_ = idx.Ingest(docRec(1, "AAPL", "x1", "", base))
	if got := idx.Recent(10); len(got) != 1 {
		t.Errorf("Recent(10) with 1 item = %d, want 1", len(got))
	}
	if got := idx.Recent(0); len(got) != 0 {
		t.Errorf("Recent(0) = %d, want 0", len(got))
	}
}

func TestKnownTickers_SortedAlpha(t *testing.T) {
	idx := NewIndex()
	base := time.Now().UTC()
	for _, ticker := range []string{"MSFT", "AAPL", "SCHW"} {
		_ = idx.Ingest(docRec(1, ticker, ticker+":x1", "", base))
	}
	tickers := idx.KnownTickers()
	want := []string{"AAPL", "MSFT", "SCHW"}
	for i, w := range want {
		if tickers[i] != w {
			t.Errorf("tickers[%d] = %q, want %q", i, tickers[i], w)
		}
	}
}

func TestLatestSeq_TracksHighest(t *testing.T) {
	idx := NewIndex()
	base := time.Now().UTC()
	_ = idx.Ingest(docRec(5, "AAPL", "a5", "", base))
	_ = idx.Ingest(docRec(2, "AAPL", "a2", "", base.Add(time.Second)))
	_ = idx.Ingest(docRec(9, "SCHW", "s9", "", base.Add(2*time.Second)))
	if got := idx.LatestSeq(); got != 9 {
		t.Errorf("LatestSeq = %d, want 9", got)
	}
}

func TestBuild_PopulatesFromStore(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := eventstore.NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now().UTC()
	rec1 := docRec(0, "AAPL", "a1", "2023-01-10", base)
	rec2 := docRec(0, "MSFT", "m1", "2023-02-20", base.Add(time.Hour))
	if _, err := store.Append(ctx, rec1.Event, rec2.Event); err != nil {
		t.Fatalf("Append: %v", err)
	}

	idx := NewIndex()
	if err := Build(ctx, store, idx, nil); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if docs := idx.ForTicker("AAPL"); len(docs) != 1 {
		t.Errorf("AAPL docs = %d, want 1", len(docs))
	}
	if docs := idx.ForTicker("MSFT"); len(docs) != 1 {
		t.Errorf("MSFT docs = %d, want 1", len(docs))
	}
}

func TestPreviewText_TruncatesAtWordBoundary(t *testing.T) {
	text := "hello world foo bar baz"
	got := previewText(text, 14)
	if got != "hello world" {
		t.Errorf("previewText = %q, want %q", got, "hello world")
	}
}

func TestPreviewText_ShortTextUnchanged(t *testing.T) {
	text := "short"
	if got := previewText(text, 100); got != text {
		t.Errorf("previewText = %q, want %q", got, text)
	}
}
