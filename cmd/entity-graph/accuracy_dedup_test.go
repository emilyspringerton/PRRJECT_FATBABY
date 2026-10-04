package main

import (
	"path/filepath"
	"testing"

	"github.com/example/prrject-fatbaby/internal/entitygraph"
)

// The correlators recompute every accuracy record each batch; only new or genuinely changed
// records may be appended to accuracy.ndjson (it once grew to 15 GB of ~190x duplicates).
func TestChangedAccuracyRecords_OnlyNewOrChanged(t *testing.T) {
	db, err := openAccuracyIndexDB(filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	logger := discardLogger()
	mk := func(id string, o entitygraph.GroundTruth, rec string) entitygraph.AccuracyRecord {
		return entitygraph.AccuracyRecord{SignalID: id, SignalType: "director_decay", Ticker: "AAPL", Outcome: o, RecordedAt: rec}
	}

	first := []entitygraph.AccuracyRecord{mk("a", entitygraph.GTPending, "2026-10-01"), mk("b", entitygraph.GTPending, "2026-10-01")}
	got := changedAccuracyRecords(db, first)
	if len(got) != 2 {
		t.Fatalf("empty index: all records are new, got %d", len(got))
	}
	if err := upsertAccuracyRecords(db, got, logger); err != nil {
		t.Fatal(err)
	}

	// Next batch recomputes the same records (new RecordedAt only) plus a changed outcome and a new one.
	again := []entitygraph.AccuracyRecord{
		mk("a", entitygraph.GTPending, "2026-10-02"),   // identical but for the date -> skip
		mk("b", entitygraph.GTConfirmed, "2026-10-02"), // outcome changed -> append
		mk("c", entitygraph.GTPending, "2026-10-02"),   // new -> append
		mk("c", entitygraph.GTPending, "2026-10-02"),   // dup within the batch -> once
	}
	got = changedAccuracyRecords(db, again)
	if len(got) != 2 || got[0].SignalID != "b" || got[1].SignalID != "c" {
		t.Fatalf("want [b c], got %+v", got)
	}

	// 100 more identical recomputations append nothing.
	if err := upsertAccuracyRecords(db, got, logger); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		if n := len(changedAccuracyRecords(db, again)); n != 0 {
			t.Fatalf("iteration %d: steady state must append nothing, got %d", i, n)
		}
	}
}
