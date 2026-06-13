// Package store provides the FatBaby MySQL read-model store and a SQLite
// compatibility layer for local development without a running MySQL instance.
//
// The canonical schema lives in migrations/mysql/ as MySQL DDL. RunSQLiteMigrations
// translates those files to SQLite-compatible SQL on the fly using a regex
// translator and applies them to the given SQLite database. This means:
//   - There is one golden set of migration files (MySQL).
//   - Local dev and CI use SQLite with zero external dependencies.
//   - Production uses real MySQL; the same migration files are applied by the
//     projector's applyMigrations function.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// OpenSQLite opens (or creates) a SQLite database at path.
// Pass ":memory:" for tests.
func OpenSQLite(path string) (*sql.DB, error) {
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, fmt.Errorf("create db dir: %w", err)
		}
	}
	// WAL mode + busy timeout make concurrent reads and a single writer safe.
	db, err := sql.Open("sqlite", path+"?_foreign_keys=on&_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

// RunSQLiteMigrations applies all *.sql files in migrationsDir to db,
// translating MySQL DDL to SQLite-compatible SQL on the fly.
// It is idempotent: already-applied migrations (tracked by filename in
// schema_migrations) are skipped.
func RunSQLiteMigrations(db *sql.DB, migrationsDir string) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		filename   TEXT NOT NULL PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		return fmt.Errorf("bootstrap schema_migrations: %w", err)
	}

	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("read migrations dir %s: %w", migrationsDir, err)
	}

	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".sql" {
			continue
		}

		var applied bool
		err := db.QueryRow(`SELECT 1 FROM schema_migrations WHERE filename=?`, e.Name()).Scan(&applied)
		if err == nil {
			continue // already applied
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("check migration %s: %w", e.Name(), err)
		}

		raw, err := os.ReadFile(filepath.Join(migrationsDir, e.Name()))
		if err != nil {
			return fmt.Errorf("read migration %s: %w", e.Name(), err)
		}

		translated := mysqlToSQLite(string(raw))
		for _, stmt := range splitStatements(translated) {
			if _, err := db.Exec(stmt); err != nil {
				return fmt.Errorf("apply migration %s: %w\nSQL: %s", e.Name(), err, stmt)
			}
		}

		_, err = db.Exec(
			`INSERT INTO schema_migrations (filename, applied_at) VALUES (?, ?)`,
			e.Name(), time.Now().UTC().Format(time.RFC3339),
		)
		if err != nil {
			return fmt.Errorf("record migration %s: %w", e.Name(), err)
		}
	}
	return nil
}
