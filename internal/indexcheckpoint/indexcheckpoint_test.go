package indexcheckpoint

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/example/prrject-fatbaby/internal/newssite/docindex"
	"github.com/example/prrject-fatbaby/internal/signalindex"
)

func discardLogger() *log.Logger { return log.New(io.Discard, "", 0) }

func TestOpen_FreshFileHasEmptyCheckpoint(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"), discardLogger())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	if got := db.SignalsLatestSeq(); got != 0 {
		t.Errorf("SignalsLatestSeq = %d, want 0 for a fresh checkpoint", got)
	}
	entries, err := db.LoadSignals()
	if err != nil {
		t.Fatalf("LoadSignals: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("LoadSignals on fresh checkpoint = %d entries, want 0", len(entries))
	}
}

func TestSaveSignals_LoadSignals_Roundtrip(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"), discardLogger())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	entries := []*signalindex.SignalEntry{
		{
			Seq: 42, Ticker: "AAPL", AccessionNumber: "0001-26-000001", Form: "8-K",
			FilingDate: "2026-07-19", SignalType: "guidance_raise", Importance: 8,
			Sentiment: 0.75, Summary: "Raised guidance", ImpactAnalysis: "Positive",
			Timestamp: time.Date(2026, 7, 19, 10, 0, 0, 0, time.UTC),
			AppendedAt: time.Date(2026, 7, 19, 10, 1, 0, 0, time.UTC),
			RawMetadata: map[string]string{"form": "8-K", "filing_date": "2026-07-19"},
		},
		{
			Seq: 43, Ticker: "MSFT", AccessionNumber: "0002-26-000002", Form: "10-Q",
			Timestamp: time.Date(2026, 7, 19, 11, 0, 0, 0, time.UTC),
			AppendedAt: time.Date(2026, 7, 19, 11, 1, 0, 0, time.UTC),
		},
	}
	if err := db.SaveSignals(entries, 43); err != nil {
		t.Fatalf("SaveSignals: %v", err)
	}

	if got := db.SignalsLatestSeq(); got != 43 {
		t.Errorf("SignalsLatestSeq = %d, want 43", got)
	}

	loaded, err := db.LoadSignals()
	if err != nil {
		t.Fatalf("LoadSignals: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("LoadSignals = %d entries, want 2", len(loaded))
	}

	byTicker := map[string]*signalindex.SignalEntry{}
	for _, e := range loaded {
		byTicker[e.Ticker] = e
	}
	aapl, ok := byTicker["AAPL"]
	if !ok {
		t.Fatal("missing AAPL entry after roundtrip")
	}
	if aapl.Seq != 42 || aapl.SignalType != "guidance_raise" || aapl.Sentiment != 0.75 {
		t.Errorf("AAPL entry roundtripped incorrectly: %+v", aapl)
	}
	if !aapl.Timestamp.Equal(entries[0].Timestamp) {
		t.Errorf("Timestamp = %v, want %v", aapl.Timestamp, entries[0].Timestamp)
	}
	if aapl.RawMetadata["form"] != "8-K" {
		t.Errorf("RawMetadata not roundtripped: %+v", aapl.RawMetadata)
	}
}

func TestSaveSignals_UpsertReplacesExisting(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"), discardLogger())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	e := &signalindex.SignalEntry{Seq: 1, Ticker: "AAPL", AccessionNumber: "acc-1", Summary: "v1"}
	if err := db.SaveSignals([]*signalindex.SignalEntry{e}, 1); err != nil {
		t.Fatalf("SaveSignals v1: %v", err)
	}
	e2 := &signalindex.SignalEntry{Seq: 2, Ticker: "AAPL", AccessionNumber: "acc-1", Summary: "v2 replaces v1"}
	if err := db.SaveSignals([]*signalindex.SignalEntry{e2}, 2); err != nil {
		t.Fatalf("SaveSignals v2: %v", err)
	}

	loaded, err := db.LoadSignals()
	if err != nil {
		t.Fatalf("LoadSignals: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected upsert to replace, not duplicate: got %d rows", len(loaded))
	}
	if loaded[0].Summary != "v2 replaces v1" {
		t.Errorf("Summary = %q, want the v2 replacement", loaded[0].Summary)
	}
}

func TestSaveDocs_LoadDocs_Roundtrip(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"), discardLogger())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	docs := []*docindex.DocSummary{
		{
			Identity: "0001-26-000001:8-K", Ticker: "AAPL", SourceType: "sec_8k", Form: "8-K",
			DocumentURL: "https://sec.gov/doc1", BodyPreview: "preview text", CharCount: 1234,
			FilingDate: "2026-07-19", PersistedAt: time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC),
			Sequence: 100,
		},
	}
	if err := db.SaveDocs(docs, 100); err != nil {
		t.Fatalf("SaveDocs: %v", err)
	}
	if got := db.DocsLatestSeq(); got != 100 {
		t.Errorf("DocsLatestSeq = %d, want 100", got)
	}

	loaded, err := db.LoadDocs()
	if err != nil {
		t.Fatalf("LoadDocs: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("LoadDocs = %d, want 1", len(loaded))
	}
	if loaded[0].Identity != "0001-26-000001:8-K" || loaded[0].CharCount != 1234 {
		t.Errorf("doc roundtripped incorrectly: %+v", loaded[0])
	}
	if !loaded[0].PersistedAt.Equal(docs[0].PersistedAt) {
		t.Errorf("PersistedAt = %v, want %v", loaded[0].PersistedAt, docs[0].PersistedAt)
	}
}

func TestOpen_SelfHealsOnCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corrupt.db")
	if err := os.WriteFile(path, []byte("this is not a sqlite file"), 0o644); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}

	db, err := Open(path, discardLogger())
	if err != nil {
		t.Fatalf("Open should self-heal a corrupt file, got error: %v", err)
	}
	defer db.Close()

	if got := db.SignalsLatestSeq(); got != 0 {
		t.Errorf("self-healed checkpoint should start empty, SignalsLatestSeq = %d", got)
	}
	// Confirm it's actually usable after self-healing, not just non-erroring.
	if err := db.SaveSignals([]*signalindex.SignalEntry{{Seq: 1, Ticker: "X", AccessionNumber: "a"}}, 1); err != nil {
		t.Errorf("checkpoint unusable after self-heal: %v", err)
	}
}

func TestOpen_SelfHealsOnWrongSchemaVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db1, err := Open(path, discardLogger())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := db1.sqldb.Exec(`UPDATE meta SET value = '999' WHERE key = 'schema_version'`); err != nil {
		t.Fatalf("force wrong version: %v", err)
	}
	if err := db1.SaveSignals([]*signalindex.SignalEntry{{Seq: 1, Ticker: "X", AccessionNumber: "a"}}, 1); err != nil {
		t.Fatalf("seed data: %v", err)
	}
	db1.Close()

	db2, err := Open(path, discardLogger())
	if err != nil {
		t.Fatalf("Open on version-mismatched file should self-heal, got error: %v", err)
	}
	defer db2.Close()
	if got := db2.SignalsLatestSeq(); got != 0 {
		t.Errorf("version mismatch should reset the checkpoint (lose the seeded data), SignalsLatestSeq = %d", got)
	}
}

func TestSaveSignals_NilEntriesJustAdvancesWatermark(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"), discardLogger())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	if err := db.SaveSignals(nil, 50); err != nil {
		t.Fatalf("SaveSignals(nil, 50): %v", err)
	}
	if got := db.SignalsLatestSeq(); got != 50 {
		t.Errorf("SignalsLatestSeq = %d, want 50", got)
	}
}
