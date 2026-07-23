// filingindex maintains an incrementally-updated SQLite table mirroring
// buildFilingIndexes' two maps (identity -> filing date, identity -> form),
// so entity-graph's per-batch runBatch no longer needs a full event-store
// scan just to recover filing dates/forms for legacy docs.
//
// See PRRJECT_FATBABY/docs/northstar/replay-fragility.md §4c Phase 2,
// item 1: "This deletes a per-batch full-store scan of a 630MB store --
// the single largest recurring waste on the box."
//
// Design: a one-time backfill (the existing full buildFilingIndexes scan,
// run exactly once) populates the table; every batch afterward upserts only
// the filing_discovered records already present in that batch's `recs`
// (already fetched for the main loop -- no extra store read at all). The
// table is a disposable cache of the event store, same as
// internal/indexcheckpoint: deleting filings-index.db and letting the next
// batch re-backfill is always a safe operator move.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/example/prrject-fatbaby/internal/store"
	"github.com/example/prrject-fatbaby/secwatch"
)

// openFilingIndexDB opens (or creates) the filing index at path.
func openFilingIndexDB(path string) (*sql.DB, error) {
	db, err := store.OpenSQLite(path)
	if err != nil {
		return nil, fmt.Errorf("open filing index: %w", err)
	}
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT)`,
		`CREATE TABLE IF NOT EXISTS filings (identity TEXT PRIMARY KEY, filing_date TEXT, form TEXT)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			db.Close()
			return nil, fmt.Errorf("filing index schema: %w", err)
		}
	}
	return db, nil
}

// ensureFilingIndexBackfilled runs the one-time full-store scan (the
// original buildFilingIndexes behavior) if and only if it has never run
// before against this file. Idempotent and safe to call every process
// start -- a fresh/deleted filings-index.db just re-backfills once.
func ensureFilingIndexBackfilled(ctx context.Context, db *sql.DB, store eventstore.EventStore, logger *log.Logger) error {
	var done string
	err := db.QueryRow(`SELECT value FROM meta WHERE key='backfill_done'`).Scan(&done)
	if err == nil && done == "1" {
		return nil
	}

	logger.Printf("filing_index: running one-time backfill (full store scan) -- this only happens once, subsequent batches use incremental upserts")
	dates, forms := buildFilingIndexes(ctx, store, logger)

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`INSERT OR IGNORE INTO filings (identity, filing_date, form) VALUES (?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	ids := make(map[string]bool, len(dates)+len(forms))
	for id := range dates {
		ids[id] = true
	}
	for id := range forms {
		ids[id] = true
	}
	for id := range ids {
		if _, err := stmt.Exec(id, dates[id], forms[id]); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`INSERT OR REPLACE INTO meta (key, value) VALUES ('backfill_done', '1')`); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	logger.Printf("filing_index: backfill complete entries=%d", len(ids))
	return nil
}

// upsertFilingIndexFromBatch extracts filing_discovered records from recs
// (a batch already fetched by runBatch's main ReadFrom call -- no extra
// store read) and inserts any not already present. Preserves
// buildFilingIndexes' first-occurrence-wins semantics via INSERT OR IGNORE
// (a later filing_discovered for the same identity never overwrites an
// earlier one).
func upsertFilingIndexFromBatch(db *sql.DB, recs []eventstore.Record, logger *log.Logger) {
	var toInsert []struct{ id, date, form string }
	for _, r := range recs {
		if r.Event.Type != "filing_discovered" {
			continue
		}
		var ev secwatch.FilingDiscoveredEvent
		if err := json.Unmarshal(r.Event.Data, &ev); err != nil {
			continue
		}
		if ev.CIK == "" || ev.AccessionNumber == "" {
			continue
		}
		id := secwatch.FilingIdentity(ev.CIK, ev.AccessionNumber)
		toInsert = append(toInsert, struct{ id, date, form string }{id, ev.FilingDate, ev.EffectiveForm()})
	}
	if len(toInsert) == 0 {
		return
	}
	tx, err := db.Begin()
	if err != nil {
		logger.Printf("filing_index: begin tx err=%v", err)
		return
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`INSERT OR IGNORE INTO filings (identity, filing_date, form) VALUES (?, ?, ?)`)
	if err != nil {
		logger.Printf("filing_index: prepare err=%v", err)
		return
	}
	defer stmt.Close()
	for _, e := range toInsert {
		if _, err := stmt.Exec(e.id, e.date, e.form); err != nil {
			logger.Printf("filing_index: insert err=%v", err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		logger.Printf("filing_index: commit err=%v", err)
	}
}

// touchFilingIndexSnapshot updates meta.snapshot_at to now, unconditionally --
// called once per runBatch tick regardless of whether this batch contained
// any filing_discovered records, so the timestamp is a genuine "runBatch
// completed a poll successfully" heartbeat rather than only advancing when
// this specific table happens to get new rows. See docs/northstar/
// replay-fragility.md §4c/§9 Phase 3: a freshness check on meta.snapshot_at
// needs this to actually advance every poll to be able to catch a stalled
// checkpoint (write path wedged, batch hung) within minutes.
func touchFilingIndexSnapshot(db *sql.DB, logger *log.Logger) {
	if _, err := db.Exec(`INSERT OR REPLACE INTO meta (key, value) VALUES ('snapshot_at', ?)`,
		time.Now().UTC().Format(time.RFC3339)); err != nil {
		logger.Printf("filing_index: snapshot_at touch err=%v", err)
	}
}

// loadFilingIndexes reads the full table into the two maps buildFilingIndexes
// used to return, for a drop-in replacement at call sites.
func loadFilingIndexes(db *sql.DB) (dates map[string]string, forms map[string]string, err error) {
	dates = make(map[string]string)
	forms = make(map[string]string)
	rows, err := db.Query(`SELECT identity, filing_date, form FROM filings`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, date, form string
		if err := rows.Scan(&id, &date, &form); err != nil {
			return nil, nil, err
		}
		if date != "" {
			dates[id] = date
		}
		if form != "" {
			forms[id] = form
		}
	}
	return dates, forms, rows.Err()
}
