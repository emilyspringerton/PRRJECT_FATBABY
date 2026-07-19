// Package indexcheckpoint persists signalindex and docindex state to a
// local SQLite file so a process restart doesn't require replaying the
// full event store. The checkpoint is a disposable cache of the event
// store -- never a source of truth -- deleting one is always a safe
// operator move; a missing/corrupt/version-mismatched checkpoint just
// means a full rebuild via eventstore.Scan, which Phase 0 made cheap.
//
// See PRRJECT_FATBABY/docs/northstar/replay-fragility.md §4b.
package indexcheckpoint

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/example/prrject-fatbaby/internal/newssite/docindex"
	"github.com/example/prrject-fatbaby/internal/signalindex"
	"github.com/example/prrject-fatbaby/internal/store"
)

const schemaVersion = 1

// DB is one process's checkpoint file.
type DB struct {
	sqldb *sql.DB
}

// Open opens (or creates) the checkpoint file at path. If the schema is
// missing, corrupt, or at an older version, it self-heals by deleting the
// file and starting fresh with an empty schema -- callers see this as an
// empty checkpoint (LoadSignals/LoadDocs return nothing) and should do a
// full rebuild via eventstore.Scan, exactly as if this were a brand-new
// process. path may be ":memory:" for tests.
func Open(path string, logger *log.Logger) (*DB, error) {
	sqldb, err := openAndVerify(path, logger)
	if err != nil {
		return nil, err
	}
	return &DB{sqldb: sqldb}, nil
}

func openAndVerify(path string, logger *log.Logger) (*sql.DB, error) {
	sqldb, err := store.OpenSQLite(path)
	if err != nil {
		return nil, fmt.Errorf("open checkpoint: %w", err)
	}
	if err := ensureSchema(sqldb); err == nil {
		return sqldb, nil
	} else if logger != nil {
		logger.Printf("indexcheckpoint: %s unreadable/incompatible (%v) -- deleting and starting fresh", path, err)
	}
	sqldb.Close()
	if path != ":memory:" {
		_ = os.Remove(path)
	}
	sqldb, err = store.OpenSQLite(path)
	if err != nil {
		return nil, fmt.Errorf("reopen checkpoint after reset: %w", err)
	}
	if err := ensureSchema(sqldb); err != nil {
		sqldb.Close()
		return nil, fmt.Errorf("create fresh checkpoint schema: %w", err)
	}
	return sqldb, nil
}

func ensureSchema(db *sql.DB) error {
	var existing string
	err := db.QueryRow(`SELECT value FROM meta WHERE key='schema_version'`).Scan(&existing)
	switch {
	case err == nil:
		if existing != strconv.Itoa(schemaVersion) {
			return fmt.Errorf("schema_version %s != %d", existing, schemaVersion)
		}
		return nil
	case err == sql.ErrNoRows:
		return createSchema(db)
	default:
		// meta table itself doesn't exist yet, or the file isn't a valid db.
		return createSchema(db)
	}
}

func createSchema(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT)`,
		`CREATE TABLE IF NOT EXISTS docs (
			identity TEXT PRIMARY KEY, ticker TEXT, source_type TEXT, form TEXT,
			document_url TEXT, body_preview TEXT, char_count INTEGER,
			filing_date TEXT, persisted_at TEXT, seq INTEGER
		)`,
		`CREATE INDEX IF NOT EXISTS docs_ticker ON docs(ticker)`,
		`CREATE TABLE IF NOT EXISTS signals (
			ticker TEXT, accession TEXT, seq INTEGER, form TEXT, filing_date TEXT,
			signal_type TEXT, importance INTEGER, sentiment REAL, summary TEXT,
			impact_analysis TEXT, ts TEXT, appended_at TEXT, raw_metadata TEXT,
			PRIMARY KEY (ticker, accession)
		)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return fmt.Errorf("exec %q: %w", s, err)
		}
	}
	_, err := db.Exec(`INSERT OR REPLACE INTO meta(key, value) VALUES ('schema_version', ?)`, strconv.Itoa(schemaVersion))
	return err
}

// Close closes the underlying SQLite handle.
func (d *DB) Close() error { return d.sqldb.Close() }

// SignalsLatestSeq returns the checkpointed watermark for the signals
// table, or 0 if none exists yet (meaning: full rebuild required).
func (d *DB) SignalsLatestSeq() uint64 { return d.metaUint("signals_latest_seq") }

// DocsLatestSeq returns the checkpointed watermark for the docs table.
func (d *DB) DocsLatestSeq() uint64 { return d.metaUint("docs_latest_seq") }

func (d *DB) metaUint(key string) uint64 {
	var v string
	if err := d.sqldb.QueryRow(`SELECT value FROM meta WHERE key=?`, key).Scan(&v); err != nil {
		return 0
	}
	n, _ := strconv.ParseUint(v, 10, 64)
	return n
}

// LoadSignals reads every checkpointed signal row into memory.
func (d *DB) LoadSignals() ([]*signalindex.SignalEntry, error) {
	rows, err := d.sqldb.Query(`SELECT ticker, accession, seq, form, filing_date, signal_type,
		importance, sentiment, summary, impact_analysis, ts, appended_at, raw_metadata FROM signals`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*signalindex.SignalEntry
	for rows.Next() {
		var e signalindex.SignalEntry
		var ts, appendedAt, rawMeta string
		if err := rows.Scan(&e.Ticker, &e.AccessionNumber, &e.Seq, &e.Form, &e.FilingDate,
			&e.SignalType, &e.Importance, &e.Sentiment, &e.Summary, &e.ImpactAnalysis,
			&ts, &appendedAt, &rawMeta); err != nil {
			return nil, err
		}
		e.Timestamp, _ = time.Parse(time.RFC3339Nano, ts)
		e.AppendedAt, _ = time.Parse(time.RFC3339Nano, appendedAt)
		if rawMeta != "" {
			_ = json.Unmarshal([]byte(rawMeta), &e.RawMetadata)
		}
		out = append(out, &e)
	}
	return out, rows.Err()
}

// SaveSignals upserts entries and advances the signals watermark, all in
// one transaction. Safe to call with entries=nil (just advances the
// watermark) -- callers pass the full current in-memory set each time
// (idx.AllEntries()), not a delta; at this index's scale (thousands of
// rows) a full-snapshot upsert every poll is simpler and cheap, not worth
// the complexity of tracking per-poll deltas.
func (d *DB) SaveSignals(entries []*signalindex.SignalEntry, latestSeq uint64) error {
	tx, err := d.sqldb.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO signals
		(ticker, accession, seq, form, filing_date, signal_type, importance, sentiment,
		 summary, impact_analysis, ts, appended_at, raw_metadata)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, e := range entries {
		rawMeta, _ := json.Marshal(e.RawMetadata)
		if _, err := stmt.Exec(e.Ticker, e.AccessionNumber, e.Seq, e.Form, e.FilingDate,
			e.SignalType, e.Importance, e.Sentiment, e.Summary, e.ImpactAnalysis,
			e.Timestamp.Format(time.RFC3339Nano), e.AppendedAt.Format(time.RFC3339Nano), string(rawMeta)); err != nil {
			return err
		}
	}
	if err := setMetaTx(tx, "signals_latest_seq", strconv.FormatUint(latestSeq, 10)); err != nil {
		return err
	}
	if err := setMetaTx(tx, "snapshot_at", time.Now().UTC().Format(time.RFC3339)); err != nil {
		return err
	}
	return tx.Commit()
}

// LoadDocs reads every checkpointed doc row into memory.
func (d *DB) LoadDocs() ([]*docindex.DocSummary, error) {
	rows, err := d.sqldb.Query(`SELECT identity, ticker, source_type, form, document_url,
		body_preview, char_count, filing_date, persisted_at, seq FROM docs`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*docindex.DocSummary
	for rows.Next() {
		var ds docindex.DocSummary
		var persistedAt string
		if err := rows.Scan(&ds.Identity, &ds.Ticker, &ds.SourceType, &ds.Form, &ds.DocumentURL,
			&ds.BodyPreview, &ds.CharCount, &ds.FilingDate, &persistedAt, &ds.Sequence); err != nil {
			return nil, err
		}
		ds.PersistedAt, _ = time.Parse(time.RFC3339Nano, persistedAt)
		out = append(out, &ds)
	}
	return out, rows.Err()
}

// SaveDocs upserts summaries and advances the docs watermark, all in one
// transaction. Same full-snapshot-per-call shape as SaveSignals.
func (d *DB) SaveDocs(summaries []*docindex.DocSummary, latestSeq uint64) error {
	tx, err := d.sqldb.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO docs
		(identity, ticker, source_type, form, document_url, body_preview, char_count,
		 filing_date, persisted_at, seq)
		VALUES (?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, ds := range summaries {
		if _, err := stmt.Exec(ds.Identity, ds.Ticker, ds.SourceType, ds.Form, ds.DocumentURL,
			ds.BodyPreview, ds.CharCount, ds.FilingDate, ds.PersistedAt.Format(time.RFC3339Nano), ds.Sequence); err != nil {
			return err
		}
	}
	if err := setMetaTx(tx, "docs_latest_seq", strconv.FormatUint(latestSeq, 10)); err != nil {
		return err
	}
	if err := setMetaTx(tx, "snapshot_at", time.Now().UTC().Format(time.RFC3339)); err != nil {
		return err
	}
	return tx.Commit()
}

func setMetaTx(tx *sql.Tx, key, value string) error {
	_, err := tx.Exec(`INSERT OR REPLACE INTO meta(key, value) VALUES (?, ?)`, key, value)
	return err
}
