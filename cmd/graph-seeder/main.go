// cmd/graph-seeder seeds the SQLite governance_signals table from the
// entity-graph signals.ndjson file.
//
// The projector only writes to MySQL, so on a machine without MySQL the
// governance_signals table is empty and signalapi returns [] for every ticker.
// This command reads the entity-graph signals and inserts them into the
// SQLite read model (var/signalapi.db) so the API has data without MySQL.
//
// Usage:
//
//	go run ./cmd/graph-seeder                      # seed from var/entity-graph, db at var/signalapi.db
//	go run ./cmd/graph-seeder -dry-run             # count records only
//	go run ./cmd/graph-seeder -graph-dir <path> -db <path>
package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/example/prrject-fatbaby/internal/entitygraph"
	"github.com/example/prrject-fatbaby/internal/store"
)

func main() {
	graphDir := flag.String("graph-dir", filepath.Join("var", "entity-graph"), "entity-graph data directory")
	storeRoot := flag.String("store", filepath.Join("var", "secwatch"), "eventstore root (used to locate signalapi.db)")
	dbPath := flag.String("db", "", "explicit SQLite DB path (overrides -store)")
	dryRun := flag.Bool("dry-run", false, "count records only, do not insert")
	migrationsDir := flag.String("migrations-dir", filepath.Join("migrations", "mysql"), "MySQL migrations directory")
	flag.Parse()

	logger := log.New(os.Stdout, "graph-seeder ", log.LstdFlags|log.LUTC)

	resolvedDB := *dbPath
	if resolvedDB == "" {
		resolvedDB = filepath.Join(filepath.Dir(*storeRoot), "signalapi.db")
	}

	sigs, err := entitygraph.LoadSignals(*graphDir)
	if err != nil {
		logger.Fatalf("load signals: %v", err)
	}
	logger.Printf("loaded %d signals from %s", len(sigs), *graphDir)

	if *dryRun {
		typeCounts := map[string]int{}
		for _, s := range sigs {
			typeCounts[string(s.Type)]++
		}
		for t, n := range typeCounts {
			logger.Printf("  type=%-40s count=%d", t, n)
		}
		logger.Printf("dry-run: would insert up to %d rows into %s", len(sigs), resolvedDB)
		return
	}

	db, err := store.OpenSQLite(resolvedDB)
	if err != nil {
		logger.Fatalf("open sqlite %s: %v", resolvedDB, err)
	}
	defer db.Close()

	if err := store.RunSQLiteMigrations(db, *migrationsDir); err != nil {
		logger.Fatalf("run migrations: %v", err)
	}
	logger.Printf("SQLite ready at %s", resolvedDB)

	inserted, skipped, failed := seedSignals(db, sigs, logger)
	logger.Printf("done: inserted=%d skipped=%d failed=%d", inserted, skipped, failed)
}

func seedSignals(db *sql.DB, sigs []entitygraph.Signal, logger *log.Logger) (inserted, skipped, failed int) {
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")

	for _, s := range sigs {
		if s.SignalID == "" || s.Ticker == "" {
			skipped++
			continue
		}

		filingDate := s.FilingDate
		if filingDate == "" {
			filingDate = s.DetectedAt
		}
		if filingDate == "" {
			filingDate = now[:10]
		}

		rawJSON, _ := json.Marshal(s)

		var existing int
		if err := db.QueryRow("SELECT COUNT(*) FROM governance_signals WHERE filing_id = ?", s.SignalID).Scan(&existing); err != nil {
			logger.Printf("check exist signal_id=%s: %v", s.SignalID, err)
			failed++
			continue
		}
		if existing > 0 {
			skipped++
			continue
		}

		_, err := db.Exec(`
			INSERT INTO governance_signals
				(ticker, event_type, filing_id, entity_name, signal_score, headline, raw_json, filing_date, ingested_at, eventstore_seq)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0)`,
			s.Ticker,
			string(s.Type),
			s.SignalID,
			s.Entity,
			s.Score,
			truncate(s.Interpretation, 512),
			string(rawJSON),
			filingDate,
			now,
		)
		if err != nil {
			logger.Printf("insert signal_id=%s ticker=%s: %v", s.SignalID, s.Ticker, err)
			failed++
			continue
		}
		inserted++
	}
	return
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
