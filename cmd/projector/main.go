// projector tails the secwatch eventstore and projects signal_generated events
// into MySQL read-model tables for the Ask Emily query layer.
//
// Projections:
//
//	governance_signals — one row per signal_generated event
//	entity_timeline    — one row per signal that represents a named-entity change
//	                     (auditor_change, leadership_departure, cfo_departure, etc.)
//	market_data_daily  — one row per market_data_tick event (Yahoo Finance OHLCV)
//	                     populated when -market-store is set
//
// The projector is idempotent: it uses eventstore_seq as a dedup key, so
// replaying from cursor 0 produces the same MySQL state.
//
// Activated only when MYSQL_URL is set. Exits cleanly if the env var is absent.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/example/prrject-fatbaby/pkg/intelligence"
)

func main() {
	storeRoot := flag.String("store", filepath.Join("var", "secwatch"), "secwatch eventstore root")
	marketRoot := flag.String("market-store", "", "market-data eventstore root (optional; enables market_data_daily projection)")
	pollInterval := flag.Duration("poll-interval", 2*time.Second, "eventstore poll interval")
	batchSize := flag.Int("batch-size", 256, "max events per poll")
	oneShot := flag.Bool("one-shot", false, "process one batch and exit")
	flag.Parse()

	logger := log.New(os.Stdout, "projector ", log.LstdFlags|log.LUTC)

	mysqlURL := os.Getenv("MYSQL_URL")
	if mysqlURL == "" {
		logger.Fatal("MYSQL_URL not set — projector requires MySQL; set MYSQL_URL=user:pass@tcp(host:3306)/dbname")
	}

	db, err := sql.Open("mysql", mysqlURL+"?parseTime=true&multiStatements=true")
	if err != nil {
		logger.Fatalf("open mysql: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(4)
	db.SetConnMaxLifetime(5 * time.Minute)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := applyMigrations(ctx, db, logger); err != nil {
		logger.Fatalf("migrations: %v", err)
	}

	store, err := eventstore.NewFileStore(*storeRoot)
	if err != nil {
		logger.Fatalf("open eventstore: %v", err)
	}
	defer store.Close()

	p := &projector{db: db, logger: logger}

	cursor, err := p.loadCursor(ctx, "main")
	if err != nil {
		logger.Fatalf("load cursor: %v", err)
	}
	logger.Printf("starting governance cursor=%d poll=%s batch=%d", cursor, *pollInterval, *batchSize)

	// Optional: tail market-data eventstore for OHLCV projection.
	var marketStore eventstore.EventStore
	var marketCursor uint64
	if *marketRoot != "" {
		ms, err := eventstore.NewFileStore(*marketRoot)
		if err != nil {
			logger.Fatalf("open market-store: %v", err)
		}
		defer ms.Close()
		marketStore = ms
		marketCursor, err = p.loadCursor(ctx, "market")
		if err != nil {
			logger.Fatalf("load market cursor: %v", err)
		}
		logger.Printf("market-data store enabled cursor=%d", marketCursor)
	}

	for {
		cursor, err = p.runBatch(ctx, store, cursor, *batchSize)
		if err != nil {
			logger.Printf("governance batch error: %v", err)
		}

		if marketStore != nil {
			marketCursor, err = p.runMarketBatch(ctx, marketStore, marketCursor, *batchSize)
			if err != nil {
				logger.Printf("market batch error: %v", err)
			}
		}

		if *oneShot {
			logger.Printf("one-shot complete governance=%d market=%d", cursor, marketCursor)
			return
		}
		select {
		case <-ctx.Done():
			logger.Printf("shutdown governance=%d market=%d", cursor, marketCursor)
			return
		case <-time.After(*pollInterval):
		}
	}
}

// applyMigrations runs the SQL files in migrations/mysql/ in filename order.
func applyMigrations(ctx context.Context, db *sql.DB, logger *log.Logger) error {
	migrationsDir := filepath.Join("migrations", "mysql")
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("read migrations dir %s: %w", migrationsDir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		path := filepath.Join(migrationsDir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		if _, err := db.ExecContext(ctx, string(data)); err != nil {
			return fmt.Errorf("exec %s: %w", path, err)
		}
		logger.Printf("migration applied: %s", e.Name())
	}
	return nil
}

type projector struct {
	db     *sql.DB
	logger *log.Logger
}

func (p *projector) loadCursor(ctx context.Context, name string) (uint64, error) {
	var seq uint64
	err := p.db.QueryRowContext(ctx,
		"SELECT last_seq FROM projector_cursors WHERE projector_name = ?", name,
	).Scan(&seq)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return seq, err
}

func (p *projector) saveCursor(ctx context.Context, name string, seq uint64) error {
	_, err := p.db.ExecContext(ctx,
		"INSERT INTO projector_cursors (projector_name, last_seq) VALUES (?, ?)"+
			" ON DUPLICATE KEY UPDATE last_seq = VALUES(last_seq)",
		name, seq,
	)
	return err
}

func (p *projector) runBatch(ctx context.Context, store eventstore.EventStore, cursor uint64, batchSize int) (uint64, error) {
	recs, err := store.ReadFrom(ctx, cursor, batchSize)
	if err != nil {
		return cursor, fmt.Errorf("read from eventstore: %w", err)
	}
	if len(recs) == 0 {
		return cursor, nil
	}

	projected := 0
	for _, rec := range recs {
		if rec.Event.Type != "signal_generated" {
			cursor = rec.Sequence
			continue
		}
		var sig intelligence.Signal
		if err := json.Unmarshal(rec.Event.Data, &sig); err != nil {
			p.logger.Printf("skip seq=%d unmarshal: %v", rec.Sequence, err)
			cursor = rec.Sequence
			continue
		}
		if err := p.projectSignal(ctx, rec, sig); err != nil {
			// Log + skip: don't advance cursor so we retry next cycle.
			p.logger.Printf("project seq=%d ticker=%s: %v", rec.Sequence, sig.Ticker, err)
			break
		}
		cursor = rec.Sequence
		projected++
	}

	if projected > 0 {
		if err := p.saveCursor(ctx, "main", cursor); err != nil {
			return cursor, fmt.Errorf("save cursor: %w", err)
		}
		p.logger.Printf("governance projected=%d cursor=%d", projected, cursor)
	}
	return cursor, nil
}

// runMarketBatch projects market_data_tick events into market_data_daily.
func (p *projector) runMarketBatch(ctx context.Context, store eventstore.EventStore, cursor uint64, batchSize int) (uint64, error) {
	recs, err := store.ReadFrom(ctx, cursor, batchSize)
	if err != nil {
		return cursor, fmt.Errorf("read from market store: %w", err)
	}
	if len(recs) == 0 {
		return cursor, nil
	}

	projected := 0
	for _, rec := range recs {
		if rec.Event.Type != "market_data_tick" {
			cursor = rec.Sequence
			continue
		}
		if err := p.projectMarketBar(ctx, rec); err != nil {
			p.logger.Printf("market project seq=%d: %v", rec.Sequence, err)
			break
		}
		cursor = rec.Sequence
		projected++
	}

	if projected > 0 {
		if err := p.saveCursor(ctx, "market", cursor); err != nil {
			return cursor, fmt.Errorf("save market cursor: %w", err)
		}
		p.logger.Printf("market projected=%d cursor=%d", projected, cursor)
	}
	return cursor, nil
}

type marketBar struct {
	Ticker   string  `json:"ticker"`
	Date     string  `json:"date"`
	Open     float64 `json:"open"`
	High     float64 `json:"high"`
	Low      float64 `json:"low"`
	Close    float64 `json:"close"`
	AdjClose float64 `json:"adj_close"`
	Volume   int64   `json:"volume"`
}

func (p *projector) projectMarketBar(ctx context.Context, rec eventstore.Record) error {
	var bar marketBar
	if err := json.Unmarshal(rec.Event.Data, &bar); err != nil {
		return fmt.Errorf("unmarshal market bar: %w", err)
	}
	_, err := p.db.ExecContext(ctx, `
		INSERT INTO market_data_daily
		    (ticker, trade_date, open, high, low, close, adj_close, volume, eventstore_seq)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
		    open           = VALUES(open),
		    high           = VALUES(high),
		    low            = VALUES(low),
		    close          = VALUES(close),
		    adj_close      = VALUES(adj_close),
		    volume         = VALUES(volume)
	`,
		bar.Ticker, bar.Date,
		bar.Open, bar.High, bar.Low, bar.Close, bar.AdjClose, bar.Volume,
		rec.Sequence,
	)
	if err != nil {
		return fmt.Errorf("insert market_data_daily: %w", err)
	}
	return nil
}

// signalScore maps importance (1-10) to a 0.0–1.0 float.
func signalScore(importance int) float64 {
	if importance <= 0 {
		return 0
	}
	if importance >= 10 {
		return 1.0
	}
	return float64(importance) / 10.0
}

// entityTimelineTypes are signal types that represent named-entity changes
// and should be written to entity_timeline in addition to governance_signals.
var entityTimelineTypes = map[string]struct {
	entityType string
	eventType  string
}{
	"auditor_change":       {entityType: "auditor", eventType: "changed"},
	"leadership_departure": {entityType: "director", eventType: "resigned"},
	"cfo_departure":        {entityType: "director", eventType: "resigned"},
	"director_friction":    {entityType: "director", eventType: "flagged"},
	"director_decay":       {entityType: "director", eventType: "flagged"},
	"director_long_tenure": {entityType: "director", eventType: "flagged"},
	"nomination_rejection": {entityType: "director", eventType: "rejected"},
	"activist_risk":        {entityType: "activist", eventType: "flagged"},
	"insider_buy":          {entityType: "director", eventType: "insider_buy"},
	"insider_sell_cluster": {entityType: "director", eventType: "insider_sell"},
}

func (p *projector) projectSignal(ctx context.Context, rec eventstore.Record, sig intelligence.Signal) error {
	rawJSON, _ := json.Marshal(sig)

	// Derive filing_date: prefer sig.Timestamp, fall back to rec.AppendedAt.
	filingDate := sig.Timestamp
	if filingDate.IsZero() {
		filingDate = rec.AppendedAt
	}

	// Extract accession number from metadata if present.
	accession := ""
	if sig.RawMetadata != nil {
		accession = sig.RawMetadata["accession_number"]
	}
	if accession == "" {
		// Fall back to event Identity field.
		accession = rec.Event.Identity.AccessionNumber
	}

	entityName := ""
	sourcePublishedAt := ""
	if sig.RawMetadata != nil {
		entityName = sig.RawMetadata["entity_name"]
		if entityName == "" {
			entityName = sig.RawMetadata["director_name"]
		}
		if entityName == "" {
			entityName = sig.RawMetadata["auditor_name"]
		}
		// Prefer the explicit source_published_at stamp; fall back to filing_date.
		sourcePublishedAt = sig.RawMetadata["source_published_at"]
		if sourcePublishedAt == "" {
			sourcePublishedAt = sig.RawMetadata["filing_date"]
		}
	}
	// Last resort: use the signal timestamp date.
	if sourcePublishedAt == "" {
		sourcePublishedAt = filingDate.Format("2006-01-02")
	}

	score := signalScore(sig.Importance)

	// S175-02: skuldmark_id rides RawMetadata the same way source_published_at
	// does above -- populated by internal/processor/worker.go from the
	// filing_discovered event's identity, empty (not guessed) when the
	// filing predates S175-01 or its exchange couldn't be resolved at mint
	// time. sql.NullString so an empty string projects as SQL NULL, not "".
	var skuldmarkID sql.NullString
	if sig.RawMetadata != nil && sig.RawMetadata["skuldmark_id"] != "" {
		skuldmarkID = sql.NullString{String: sig.RawMetadata["skuldmark_id"], Valid: true}
	}

	_, err := p.db.ExecContext(ctx, `
		INSERT INTO governance_signals
		    (ticker, event_type, filing_id, entity_name, skuldmark_id, signal_score, headline, raw_json, filing_date, source_published_at, eventstore_seq)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
		    signal_score       = VALUES(signal_score),
		    headline           = VALUES(headline),
		    raw_json           = VALUES(raw_json),
		    source_published_at = VALUES(source_published_at),
		    skuldmark_id       = VALUES(skuldmark_id)
	`,
		sig.Ticker,
		sig.SignalType,
		accession,
		entityName,
		skuldmarkID,
		score,
		truncate(sig.Summary, 511),
		rawJSON,
		filingDate.Format("2006-01-02"),
		sourcePublishedAt,
		rec.Sequence,
	)
	if err != nil {
		return fmt.Errorf("insert governance_signals: %w", err)
	}

	// Additionally write entity_timeline for named-entity change signals.
	if meta, ok := entityTimelineTypes[sig.SignalType]; ok {
		_, err = p.db.ExecContext(ctx, `
			INSERT INTO entity_timeline
			    (ticker, entity_name, entity_type, event_type, event_date, role, description, source_filing, skuldmark_id, eventstore_seq)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE
			    description  = VALUES(description),
			    skuldmark_id = VALUES(skuldmark_id)
		`,
			sig.Ticker,
			entityName,
			meta.entityType,
			meta.eventType,
			filingDate.Format("2006-01-02"),
			"",
			truncate(sig.Summary, 4000),
			accession,
			skuldmarkID,
			rec.Sequence,
		)
		if err != nil {
			return fmt.Errorf("insert entity_timeline: %w", err)
		}
	}
	return nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
