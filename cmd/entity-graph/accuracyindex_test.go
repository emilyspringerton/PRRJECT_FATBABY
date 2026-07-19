package main

import (
	"path/filepath"
	"testing"

	"github.com/example/prrject-fatbaby/internal/entitygraph"
)

func TestOpenAccuracyIndexDB_CreatesSchema(t *testing.T) {
	db, err := openAccuracyIndexDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("openAccuracyIndexDB: %v", err)
	}
	defer db.Close()

	records, err := loadAccuracyIndex(db)
	if err != nil {
		t.Fatalf("loadAccuracyIndex: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("fresh db should be empty, got %d records", len(records))
	}
}

// TestUpsertAccuracyRecords_LatestWins is the core correctness property this
// whole package exists for: a signal's verdict changing from pending to
// confirmed across multiple upserts must replace the old row, not add a
// second one -- otherwise this is exactly the same duplication bug as
// accuracy.ndjson, just moved into SQLite.
func TestUpsertAccuracyRecords_LatestWins(t *testing.T) {
	db, err := openAccuracyIndexDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("openAccuracyIndexDB: %v", err)
	}
	defer db.Close()
	logger := discardLogger()

	// Same (signal_id, signal_type) upserted 3 times, as if 3 batches ran
	// while the prediction was pending and then resolved.
	batch1 := []entitygraph.AccuracyRecord{
		{SignalID: "sig-1", SignalType: "director_decay", Outcome: entitygraph.GTPending, RecordedAt: "2026-07-01"},
	}
	batch2 := []entitygraph.AccuracyRecord{
		{SignalID: "sig-1", SignalType: "director_decay", Outcome: entitygraph.GTPending, RecordedAt: "2026-07-02"},
	}
	batch3 := []entitygraph.AccuracyRecord{
		{SignalID: "sig-1", SignalType: "director_decay", Outcome: entitygraph.GTConfirmed, EvidenceDate: "2026-07-03", RecordedAt: "2026-07-03"},
	}

	for _, batch := range [][]entitygraph.AccuracyRecord{batch1, batch2, batch3} {
		if err := upsertAccuracyRecords(db, batch, logger); err != nil {
			t.Fatalf("upsertAccuracyRecords: %v", err)
		}
	}

	records, err := loadAccuracyIndex(db)
	if err != nil {
		t.Fatalf("loadAccuracyIndex: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected exactly 1 deduplicated record for sig-1/director_decay, got %d", len(records))
	}
	if records[0].Outcome != entitygraph.GTConfirmed {
		t.Errorf("expected latest outcome 'confirmed', got %q", records[0].Outcome)
	}
	if records[0].RecordedAt != "2026-07-03" {
		t.Errorf("expected latest recorded_at '2026-07-03', got %q", records[0].RecordedAt)
	}
}

// TestUpsertAccuracyRecords_DistinctSignalTypesKeptSeparate verifies the
// composite key is (signal_id, signal_type), not signal_id alone -- the same
// signal_id could in principle be evaluated by more than one correlate
// function against different evidence types.
func TestUpsertAccuracyRecords_DistinctSignalTypesKeptSeparate(t *testing.T) {
	db, err := openAccuracyIndexDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("openAccuracyIndexDB: %v", err)
	}
	defer db.Close()

	records := []entitygraph.AccuracyRecord{
		{SignalID: "sig-1", SignalType: "director_decay", Outcome: entitygraph.GTConfirmed},
		{SignalID: "sig-1", SignalType: "auditor_change", Outcome: entitygraph.GTRefuted},
	}
	if err := upsertAccuracyRecords(db, records, discardLogger()); err != nil {
		t.Fatalf("upsertAccuracyRecords: %v", err)
	}

	got, err := loadAccuracyIndex(db)
	if err != nil {
		t.Fatalf("loadAccuracyIndex: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 records (distinct signal_type), got %d", len(got))
	}
}

// TestCalibrationEquivalence_DedupedReportIsMathematicallyCorrect is the
// "calibration-equivalence gate" the backlog called for (SECTION 1 Phase
// 2b). It deliberately does NOT assert the deduplicated precision equals the
// old duplicate-counted precision -- measured against the real production
// accuracy.ndjson (2026-07-19), those numbers are 18.16% (deduped) vs 12.55%
// (duplicate-counted, and drifting upward over time as more batches ran) --
// a real, substantial difference, not noise. Asserting equivalence to the
// old number would mean asserting a known-wrong value. What this test
// actually gates: that after deduplication, BuildAccuracyReports produces
// the mathematically correct precision for the *unique* signals, unpolluted
// by how many times each one happened to be re-evaluated before this fix.
func TestCalibrationEquivalence_DedupedReportIsMathematicallyCorrect(t *testing.T) {
	db, err := openAccuracyIndexDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("openAccuracyIndexDB: %v", err)
	}
	defer db.Close()
	logger := discardLogger()

	// Simulate accuracy.ndjson's real shape: a handful of unique signals,
	// each re-emitted a different number of times (as director_decay was
	// measured at ~546x on average, some far more than others), with some
	// transitioning outcome partway through -- exactly the pattern that
	// makes duplicate-counting bias the aggregate toward early-resolving
	// signals. 4 unique signals here: 2 confirmed, 1 refuted, 1 pending.
	// True precision = confirmed / (confirmed + refuted) = 2/3 = 0.6667.
	var allBatches [][]entitygraph.AccuracyRecord

	// sig-A: confirmed from the very first batch, re-emitted 50 times
	// identically (mimics an early, stable resolution).
	for i := 0; i < 50; i++ {
		allBatches = append(allBatches, []entitygraph.AccuracyRecord{
			{SignalID: "sig-A", SignalType: "director_decay", Outcome: entitygraph.GTConfirmed},
		})
	}
	// sig-B: pending for 40 batches, then confirmed for the last 2 -- if
	// duplicate-counted, this contributes 40 "pending" + 2 "confirmed"
	// entries; deduplicated, it's just 1 confirmed.
	for i := 0; i < 40; i++ {
		allBatches = append(allBatches, []entitygraph.AccuracyRecord{
			{SignalID: "sig-B", SignalType: "director_decay", Outcome: entitygraph.GTPending},
		})
	}
	for i := 0; i < 2; i++ {
		allBatches = append(allBatches, []entitygraph.AccuracyRecord{
			{SignalID: "sig-B", SignalType: "director_decay", Outcome: entitygraph.GTConfirmed},
		})
	}
	// sig-C: refuted once, stays refuted for 10 more batches.
	for i := 0; i < 11; i++ {
		allBatches = append(allBatches, []entitygraph.AccuracyRecord{
			{SignalID: "sig-C", SignalType: "director_decay", Outcome: entitygraph.GTRefuted},
		})
	}
	// sig-D: still pending, only evaluated once so far.
	allBatches = append(allBatches, []entitygraph.AccuracyRecord{
		{SignalID: "sig-D", SignalType: "director_decay", Outcome: entitygraph.GTPending},
	})

	for _, batch := range allBatches {
		if err := upsertAccuracyRecords(db, batch, logger); err != nil {
			t.Fatalf("upsertAccuracyRecords: %v", err)
		}
	}

	// Sanity: the raw (undeduplicated) input had 104 records for 4 unique
	// signals -- same shape of inflation as the real file.
	totalRaw := 0
	for _, b := range allBatches {
		totalRaw += len(b)
	}
	if totalRaw != 104 {
		t.Fatalf("test setup error: expected 104 raw records, got %d", totalRaw)
	}

	deduped, err := loadAccuracyIndex(db)
	if err != nil {
		t.Fatalf("loadAccuracyIndex: %v", err)
	}
	if len(deduped) != 4 {
		t.Fatalf("expected 4 deduplicated records (sig-A..D), got %d", len(deduped))
	}

	reports := entitygraph.BuildAccuracyReports(deduped)
	if len(reports) != 1 {
		t.Fatalf("expected 1 report (single signal_type), got %d", len(reports))
	}
	rpt := reports[0]

	if rpt.TotalPredictions != 4 {
		t.Errorf("TotalPredictions: got %d, want 4 (not 104 -- duplicate-counting was the bug this fixes)", rpt.TotalPredictions)
	}
	if rpt.Confirmed != 2 {
		t.Errorf("Confirmed: got %d, want 2 (sig-A, sig-B)", rpt.Confirmed)
	}
	if rpt.Refuted != 1 {
		t.Errorf("Refuted: got %d, want 1 (sig-C)", rpt.Refuted)
	}
	if rpt.Pending != 1 {
		t.Errorf("Pending: got %d, want 1 (sig-D)", rpt.Pending)
	}
	wantPrecision := 2.0 / 3.0
	if diff := rpt.Precision - wantPrecision; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("Precision: got %.6f, want %.6f (2 confirmed / 3 resolved)", rpt.Precision, wantPrecision)
	}
}

// TestEnsureAccuracyIndexBackfilled_IdempotentAndDeduplicates verifies the
// one-time backfill (a) only runs once against a given db file and (b)
// correctly collapses duplicate lines in accuracy.ndjson, keeping the last
// one in file order -- matching what a live process incrementally upserting
// would have converged to.
func TestEnsureAccuracyIndexBackfilled_IdempotentAndDeduplicates(t *testing.T) {
	graphDir := t.TempDir()
	if err := entitygraph.WriteAccuracyRecords(graphDir, []entitygraph.AccuracyRecord{
		{SignalID: "sig-1", SignalType: "director_decay", Outcome: entitygraph.GTPending, RecordedAt: "2026-07-01"},
	}); err != nil {
		t.Fatalf("seed ndjson: %v", err)
	}
	if err := entitygraph.WriteAccuracyRecords(graphDir, []entitygraph.AccuracyRecord{
		{SignalID: "sig-1", SignalType: "director_decay", Outcome: entitygraph.GTConfirmed, RecordedAt: "2026-07-02"},
	}); err != nil {
		t.Fatalf("seed ndjson: %v", err)
	}

	db, err := openAccuracyIndexDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("openAccuracyIndexDB: %v", err)
	}
	defer db.Close()
	logger := discardLogger()

	if err := ensureAccuracyIndexBackfilled(db, graphDir, logger); err != nil {
		t.Fatalf("ensureAccuracyIndexBackfilled: %v", err)
	}
	records, err := loadAccuracyIndex(db)
	if err != nil {
		t.Fatalf("loadAccuracyIndex: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 deduplicated record after backfill, got %d", len(records))
	}
	if records[0].Outcome != entitygraph.GTConfirmed {
		t.Errorf("expected backfill to keep the latest (confirmed) outcome, got %q", records[0].Outcome)
	}

	// Second call must be a no-op (idempotent) -- append a third, different
	// NDJSON record after the first backfill and confirm it is NOT picked
	// up by re-running ensureAccuracyIndexBackfilled (only incremental
	// upserts from live batches should add it, matching production
	// behavior where the backfill flag is sticky).
	if err := entitygraph.WriteAccuracyRecords(graphDir, []entitygraph.AccuracyRecord{
		{SignalID: "sig-2", SignalType: "director_decay", Outcome: entitygraph.GTRefuted, RecordedAt: "2026-07-03"},
	}); err != nil {
		t.Fatalf("seed ndjson: %v", err)
	}
	if err := ensureAccuracyIndexBackfilled(db, graphDir, logger); err != nil {
		t.Fatalf("second ensureAccuracyIndexBackfilled: %v", err)
	}
	records, err = loadAccuracyIndex(db)
	if err != nil {
		t.Fatalf("loadAccuracyIndex: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected backfill to be idempotent (still 1 record, sig-2 not picked up), got %d", len(records))
	}
}
