package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log"
	"path/filepath"
	"testing"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/example/prrject-fatbaby/secwatch"
)

func discardLogger() *log.Logger { return log.New(io.Discard, "", 0) }

func filingDiscoveredRecord(seq uint64, cik, accession, filingDate, form string) eventstore.Record {
	ev := secwatch.FilingDiscoveredEvent{CIK: cik, AccessionNumber: accession, FilingDate: filingDate, Form: form}
	b, _ := json.Marshal(ev)
	return eventstore.Record{
		Sequence: seq,
		Event:    eventstore.Event{Type: "filing_discovered", Data: b},
	}
}

func TestOpenFilingIndexDB_CreatesSchema(t *testing.T) {
	db, err := openFilingIndexDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("openFilingIndexDB: %v", err)
	}
	defer db.Close()

	dates, forms, err := loadFilingIndexes(db)
	if err != nil {
		t.Fatalf("loadFilingIndexes: %v", err)
	}
	if len(dates) != 0 || len(forms) != 0 {
		t.Errorf("fresh db should be empty, got dates=%d forms=%d", len(dates), len(forms))
	}
}

func TestEnsureFilingIndexBackfilled_PopulatesFromStore(t *testing.T) {
	storeDir := t.TempDir()
	evStore, err := eventstore.NewFileStore(storeDir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer evStore.Close()

	_, err = evStore.Append(context.Background(),
		mustEvent(t, "ev-1", "filing_discovered", secwatch.FilingDiscoveredEvent{CIK: "1", AccessionNumber: "a", FilingDate: "2026-01-01", Form: "8-K"}),
		mustEvent(t, "ev-2", "filing_discovered", secwatch.FilingDiscoveredEvent{CIK: "2", AccessionNumber: "b", FilingDate: "2026-01-02", Form: "10-Q"}),
	)
	if err != nil {
		t.Fatalf("append: %v", err)
	}

	db, err := openFilingIndexDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("openFilingIndexDB: %v", err)
	}
	defer db.Close()

	if err := ensureFilingIndexBackfilled(context.Background(), db, evStore, discardLogger()); err != nil {
		t.Fatalf("ensureFilingIndexBackfilled: %v", err)
	}

	dates, forms, err := loadFilingIndexes(db)
	if err != nil {
		t.Fatalf("loadFilingIndexes: %v", err)
	}
	id1 := secwatch.FilingIdentity("1", "a")
	if dates[id1] != "2026-01-01" || forms[id1] != "8-K" {
		t.Errorf("identity 1 not backfilled correctly: date=%q form=%q", dates[id1], forms[id1])
	}
	id2 := secwatch.FilingIdentity("2", "b")
	if dates[id2] != "2026-01-02" || forms[id2] != "10-Q" {
		t.Errorf("identity 2 not backfilled correctly: date=%q form=%q", dates[id2], forms[id2])
	}
}

func TestEnsureFilingIndexBackfilled_OnlyRunsOnce(t *testing.T) {
	storeDir := t.TempDir()
	evStore, err := eventstore.NewFileStore(storeDir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer evStore.Close()
	if _, err := evStore.Append(context.Background(), mustEvent(t, "ev-backfill-once-1", "filing_discovered", secwatch.FilingDiscoveredEvent{CIK: "1", AccessionNumber: "a", FilingDate: "2026-01-01", Form: "8-K"})); err != nil {
		t.Fatalf("append: %v", err)
	}

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := openFilingIndexDB(dbPath)
	if err != nil {
		t.Fatalf("openFilingIndexDB: %v", err)
	}
	defer db.Close()

	if err := ensureFilingIndexBackfilled(context.Background(), db, evStore, discardLogger()); err != nil {
		t.Fatalf("first backfill: %v", err)
	}

	// Append a record AFTER the backfill; a second call must not pick it up
	// (that's the incremental-upsert path's job, not another full scan).
	if _, err := evStore.Append(context.Background(), mustEvent(t, "ev-backfill-once-2", "filing_discovered", secwatch.FilingDiscoveredEvent{CIK: "2", AccessionNumber: "b", FilingDate: "2026-02-01", Form: "10-K"})); err != nil {
		t.Fatalf("append 2: %v", err)
	}
	if err := ensureFilingIndexBackfilled(context.Background(), db, evStore, discardLogger()); err != nil {
		t.Fatalf("second backfill call: %v", err)
	}

	dates, _, err := loadFilingIndexes(db)
	if err != nil {
		t.Fatalf("loadFilingIndexes: %v", err)
	}
	id2 := secwatch.FilingIdentity("2", "b")
	if _, present := dates[id2]; present {
		t.Error("second backfill call should be a no-op (backfill_done already set), but the post-backfill record appeared")
	}
}

func TestUpsertFilingIndexFromBatch_FirstOccurrenceWins(t *testing.T) {
	db, err := openFilingIndexDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("openFilingIndexDB: %v", err)
	}
	defer db.Close()

	recs := []eventstore.Record{
		filingDiscoveredRecord(1, "1", "a", "2026-01-01", "8-K"),
		filingDiscoveredRecord(2, "1", "a", "2026-99-99", "WRONG"), // same identity, later record
	}
	upsertFilingIndexFromBatch(db, recs, discardLogger())

	dates, forms, err := loadFilingIndexes(db)
	if err != nil {
		t.Fatalf("loadFilingIndexes: %v", err)
	}
	id := secwatch.FilingIdentity("1", "a")
	if dates[id] != "2026-01-01" {
		t.Errorf("expected first-occurrence-wins semantics (matching original buildFilingIndexes), got date=%q", dates[id])
	}
	if forms[id] != "8-K" {
		t.Errorf("expected first-occurrence-wins for form too, got form=%q", forms[id])
	}
}

func TestUpsertFilingIndexFromBatch_IgnoresNonFilingDiscoveredEvents(t *testing.T) {
	db, err := openFilingIndexDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("openFilingIndexDB: %v", err)
	}
	defer db.Close()

	recs := []eventstore.Record{
		{Sequence: 1, Event: eventstore.Event{Type: "source_document_persisted", Data: []byte(`{}`)}},
	}
	upsertFilingIndexFromBatch(db, recs, discardLogger())

	dates, forms, err := loadFilingIndexes(db)
	if err != nil {
		t.Fatalf("loadFilingIndexes: %v", err)
	}
	if len(dates) != 0 || len(forms) != 0 {
		t.Errorf("non-filing_discovered events should be ignored, got dates=%d forms=%d", len(dates), len(forms))
	}
}

func TestUpsertFilingIndexFromBatch_ThenBackfillDoesNotOverwrite(t *testing.T) {
	// Simulates the real steady-state: a batch's incremental upsert lands a
	// record, then (hypothetically, if backfill_done were somehow unset)
	// re-running backfill must not clobber it with a different value found
	// via the full scan -- INSERT OR IGNORE protects this either direction.
	storeDir := t.TempDir()
	evStore, err := eventstore.NewFileStore(storeDir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer evStore.Close()
	if _, err := evStore.Append(context.Background(), mustEvent(t, "ev-backfill-once-1", "filing_discovered", secwatch.FilingDiscoveredEvent{CIK: "1", AccessionNumber: "a", FilingDate: "2026-01-01", Form: "8-K"})); err != nil {
		t.Fatalf("append: %v", err)
	}

	db, err := openFilingIndexDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("openFilingIndexDB: %v", err)
	}
	defer db.Close()

	// Incremental upsert lands first, with a deliberately different value.
	upsertFilingIndexFromBatch(db, []eventstore.Record{
		filingDiscoveredRecord(1, "1", "a", "1999-01-01", "OLDFORM"),
	}, discardLogger())

	if err := ensureFilingIndexBackfilled(context.Background(), db, evStore, discardLogger()); err != nil {
		t.Fatalf("backfill: %v", err)
	}

	dates, _, err := loadFilingIndexes(db)
	if err != nil {
		t.Fatalf("loadFilingIndexes: %v", err)
	}
	id := secwatch.FilingIdentity("1", "a")
	if dates[id] != "1999-01-01" {
		t.Errorf("whichever value landed first should stick, got %q", dates[id])
	}
}

func mustEvent(t *testing.T, id, typ string, data any) eventstore.Event {
	t.Helper()
	b, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return eventstore.Event{ID: id, Type: typ, Data: b}
}

// TestTouchFilingIndexSnapshot_AdvancesEvenWithNoNewRows covers the Phase 3
// freshness-check gap this was written to close: a batch with zero new
// filing_discovered records must still advance meta.snapshot_at, otherwise a
// freshness check reading it would flag a perfectly healthy, idle process as
// stalled. See docs/northstar/replay-fragility.md §4c/§9 Phase 3.
func TestTouchFilingIndexSnapshot_AdvancesEvenWithNoNewRows(t *testing.T) {
	db, err := openFilingIndexDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("openFilingIndexDB: %v", err)
	}
	defer db.Close()

	var before string
	if err := db.QueryRow(`SELECT value FROM meta WHERE key='snapshot_at'`).Scan(&before); err != sql.ErrNoRows {
		t.Fatalf("expected no snapshot_at before first touch, got value=%q err=%v", before, err)
	}

	touchFilingIndexSnapshot(db, discardLogger())

	var after string
	if err := db.QueryRow(`SELECT value FROM meta WHERE key='snapshot_at'`).Scan(&after); err != nil {
		t.Fatalf("expected snapshot_at after touch, err=%v", err)
	}
	if _, err := time.Parse(time.RFC3339, after); err != nil {
		t.Errorf("snapshot_at %q not RFC3339: %v", after, err)
	}

	touchFilingIndexSnapshot(db, discardLogger())
	var again string
	if err := db.QueryRow(`SELECT value FROM meta WHERE key='snapshot_at'`).Scan(&again); err != nil {
		t.Fatalf("expected snapshot_at after second touch, err=%v", err)
	}
	if _, err := time.Parse(time.RFC3339, again); err != nil {
		t.Errorf("second snapshot_at %q not RFC3339: %v", again, err)
	}
}
