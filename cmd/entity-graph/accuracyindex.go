// accuracyindex maintains an incrementally-upserted SQLite table mirroring
// the current ground-truth verdict for each (signal_id, signal_type) pair,
// replacing the full re-scan of accuracy.ndjson that ran at every process
// start.
//
// See PRRJECT_FATBABY/docs/northstar/replay-fragility.md §4c Phase 2, item
// 2b, and the finding that motivated it: the correlate functions in
// internal/entitygraph/accuracy.go (CorrelateDecayDeparture etc.) re-emit
// one fresh AccuracyRecord for every matching historical signal on every
// single batch run, not just newly-resolved ones. accuracy.ndjson is
// append-only, so this means the file accumulates one duplicate record per
// signal per batch forever. Measured against the real production file
// (2026-07-19): 502,834 lines for only 921 unique (signal_id, signal_type)
// pairs -- a 546:1 duplication ratio.
//
// This is not just a performance problem. BuildAccuracyReports counts every
// record without deduplicating, so the aggregate precision figure the
// codebase has been tracking as "the 11.9% finding" was never a stable
// ground-truth number -- it drifts with how many batches have run (measured
// at 12.55% by the time this was built) and is systematically biased toward
// signal types that resolved early (they accumulate more duplicate
// confirmed/refuted entries every subsequent batch). The deduplicated
// (upsert-latest-wins) aggregate precision on the same data is 18.16% --
// a real, substantial difference, not noise. See the calibration-
// equivalence test in accuracyindex_test.go, which asserts the dedup logic
// is internally correct rather than asserting the old (bugged) number is
// preserved -- preserving it would mean asserting a known-wrong value.
//
// Design mirrors filingindex.go exactly: a one-time backfill from the
// existing accuracy.ndjson (upserting in file order so the last write for
// each signal wins, matching how a live process would have converged),
// then incremental per-batch upserts from that batch's freshly-computed
// accuracyRecords. accuracy.ndjson itself is left untouched and still
// appended to -- it remains the raw append-only history/audit trail (how a
// verdict evolved over time is still recoverable there); this table is
// purely the fast, deduplicated "current verdict" cache used for reports
// and RSI calibration. Same disposable-cache invariant as every other
// checkpoint in this codebase: deleting accuracy-index.db is always a safe
// operator move, the next start just re-backfills from accuracy.ndjson.
package main

import (
	"database/sql"
	"fmt"
	"log"

	"github.com/example/prrject-fatbaby/internal/entitygraph"
	"github.com/example/prrject-fatbaby/internal/store"
)

// openAccuracyIndexDB opens (or creates) the accuracy index at path.
func openAccuracyIndexDB(path string) (*sql.DB, error) {
	db, err := store.OpenSQLite(path)
	if err != nil {
		return nil, fmt.Errorf("open accuracy index: %w", err)
	}
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT)`,
		`CREATE TABLE IF NOT EXISTS accuracy_records (
			signal_id     TEXT NOT NULL,
			signal_type   TEXT NOT NULL,
			ticker        TEXT,
			predicted_at  TEXT,
			valid_through TEXT,
			outcome       TEXT,
			evidence_date TEXT,
			evidence_type TEXT,
			notes         TEXT,
			recorded_at   TEXT,
			PRIMARY KEY (signal_id, signal_type)
		)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			db.Close()
			return nil, fmt.Errorf("accuracy index schema: %w", err)
		}
	}
	return db, nil
}

// ensureAccuracyIndexBackfilled runs the one-time full-NDJSON scan (the
// original LoadAccuracyRecords behavior) if and only if it has never run
// before against this file. Idempotent and safe to call every process
// start -- a fresh/deleted accuracy-index.db just re-backfills once.
// Records are upserted in file order, so the last (most recent) record for
// each (signal_id, signal_type) pair wins, same semantics a live process
// upserting incrementally would have converged to.
func ensureAccuracyIndexBackfilled(db *sql.DB, graphDir string, logger *log.Logger) error {
	var done string
	err := db.QueryRow(`SELECT value FROM meta WHERE key='backfill_done'`).Scan(&done)
	if err == nil && done == "1" {
		return nil
	}

	logger.Printf("accuracy_index: running one-time backfill (full accuracy.ndjson scan) -- this only happens once, subsequent batches use incremental upserts")
	records, err := entitygraph.LoadAccuracyRecords(graphDir)
	if err != nil {
		return fmt.Errorf("load accuracy.ndjson for backfill: %w", err)
	}

	if err := upsertAccuracyRecords(db, records, logger); err != nil {
		return fmt.Errorf("backfill upsert: %w", err)
	}
	if _, err := db.Exec(`INSERT OR REPLACE INTO meta (key, value) VALUES ('backfill_done', '1')`); err != nil {
		return err
	}
	logger.Printf("accuracy_index: backfill complete raw_records=%d", len(records))
	return nil
}

// upsertAccuracyRecords upserts each record keyed on (signal_id,
// signal_type) -- unlike filingindex's INSERT OR IGNORE (first-occurrence-
// wins), this is INSERT ... ON CONFLICT DO UPDATE, because the whole point
// is that a signal's verdict can change (pending -> confirmed/refuted) and
// the newer verdict must replace the older one, not be ignored.
func upsertAccuracyRecords(db *sql.DB, records []entitygraph.AccuracyRecord, logger *log.Logger) error {
	if len(records) == 0 {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`
		INSERT INTO accuracy_records
			(signal_id, signal_type, ticker, predicted_at, valid_through, outcome, evidence_date, evidence_type, notes, recorded_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (signal_id, signal_type) DO UPDATE SET
			ticker=excluded.ticker, predicted_at=excluded.predicted_at, valid_through=excluded.valid_through,
			outcome=excluded.outcome, evidence_date=excluded.evidence_date, evidence_type=excluded.evidence_type,
			notes=excluded.notes, recorded_at=excluded.recorded_at
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, r := range records {
		if r.SignalID == "" {
			continue // can't upsert without a key; matches LoadAccuracyRecords tolerance of malformed lines
		}
		if _, err := stmt.Exec(r.SignalID, string(r.SignalType), r.Ticker, r.PredictedAt, r.ValidThrough,
			string(r.Outcome), r.EvidenceDate, r.EvidenceType, r.Notes, r.RecordedAt); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

// loadAccuracyIndex reads the full (deduplicated) table back into
// []entitygraph.AccuracyRecord, a drop-in replacement for
// entitygraph.LoadAccuracyRecords at call sites -- BuildAccuracyReports is
// unchanged, it just now runs over ~921 deduplicated rows instead of 502K+
// duplicated lines.
func loadAccuracyIndex(db *sql.DB) ([]entitygraph.AccuracyRecord, error) {
	rows, err := db.Query(`SELECT signal_id, signal_type, ticker, predicted_at, valid_through, outcome, evidence_date, evidence_type, notes, recorded_at FROM accuracy_records`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []entitygraph.AccuracyRecord
	for rows.Next() {
		var r entitygraph.AccuracyRecord
		var signalType, outcome string
		if err := rows.Scan(&r.SignalID, &signalType, &r.Ticker, &r.PredictedAt, &r.ValidThrough,
			&outcome, &r.EvidenceDate, &r.EvidenceType, &r.Notes, &r.RecordedAt); err != nil {
			return nil, err
		}
		r.SignalType = entitygraph.SignalType(signalType)
		r.Outcome = entitygraph.GroundTruth(outcome)
		out = append(out, r)
	}
	return out, rows.Err()
}
