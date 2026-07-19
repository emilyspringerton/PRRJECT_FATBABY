package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/example/prrject-fatbaby/internal/apiserver"
	"github.com/example/prrject-fatbaby/internal/cooccurrence"
	"github.com/example/prrject-fatbaby/internal/idunaauth"
	"github.com/example/prrject-fatbaby/internal/indexcheckpoint"
	"github.com/example/prrject-fatbaby/internal/newssite/docindex"
	"github.com/example/prrject-fatbaby/internal/signalindex"
	"github.com/example/prrject-fatbaby/internal/store"
)

func main() {
	storeRoot := flag.String("store", "var/secwatch", "")
	addr := flag.String("addr", ":9091", "")
	apiKeys := flag.String("api-keys", "", "")
	pollInterval := flag.Duration("poll-interval", 30*time.Second, "")
	maxLimit := flag.Int("max-limit", 100, "")
	readTimeout := flag.Duration("read-timeout", 10*time.Second, "")
	writeTimeout := flag.Duration("write-timeout", 30*time.Second, "")
	replayFromSeq := flag.Uint64("replay-from-seq", 1, "emergency degraded-mode lever: start index rebuild at this sequence instead of full history (see PRRJECT_FATBABY/docs/northstar/replay-fragility.md §5)")
	indexDBPath := flag.String("index-db", "", "checkpoint SQLite file (default: <store's parent dir>/signalapi-index.db); a warm checkpoint turns restart cost into a function of downtime, not full history -- see docs/northstar/replay-fragility.md §4b")
	flag.Parse()
	logger := log.New(os.Stdout, "", log.LstdFlags|log.LUTC)
	if *replayFromSeq != 1 {
		logger.Printf("WARNING: -replay-from-seq=%d — starting index below full history, degraded mode", *replayFromSeq)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	store, err := eventstore.NewFileStore(*storeRoot)
	if err != nil {
		logger.Fatalf("open store: %v", err)
	}
	defer store.Close()

	ckptPath := *indexDBPath
	if ckptPath == "" {
		ckptPath = filepath.Join(filepath.Dir(*storeRoot), "signalapi-index.db")
	}
	ckpt, err := indexcheckpoint.Open(ckptPath, logger)
	if err != nil {
		logger.Fatalf("open index checkpoint: %v", err)
	}
	defer ckpt.Close()

	idx := signalindex.NewIndex()
	docIdx := docindex.NewIndex()
	signalsFrom, docsFrom := *replayFromSeq, *replayFromSeq

	// Only trust the checkpoint in normal (non-degraded-lever) mode -- an
	// operator passing -replay-from-seq explicitly wants a controlled
	// partial rebuild from that exact point, not whatever the checkpoint
	// happens to hold.
	if *replayFromSeq == 1 {
		if cached, cerr := ckpt.LoadSignals(); cerr != nil {
			logger.Printf("WARNING: load signals checkpoint failed (%v); full rebuild", cerr)
		} else if len(cached) > 0 {
			idx.LoadEntries(cached)
			signalsFrom = ckpt.SignalsLatestSeq() + 1
			logger.Printf("signalindex warm start from checkpoint: %d entries, resuming at seq %d", len(cached), signalsFrom)
		}
		if cachedDocs, cerr := ckpt.LoadDocs(); cerr != nil {
			logger.Printf("WARNING: load docs checkpoint failed (%v); full rebuild", cerr)
		} else if len(cachedDocs) > 0 {
			docIdx.LoadSummaries(cachedDocs)
			docsFrom = ckpt.DocsLatestSeq() + 1
			logger.Printf("docindex warm start from checkpoint: %d docs, resuming at seq %d", len(cachedDocs), docsFrom)
		}
	}

	scanStart := time.Now()
	if err := signalindex.Build(ctx, store, idx, signalsFrom, logger); err != nil {
		logger.Fatalf("build index: %v", err)
	}
	scanTook := time.Since(scanStart)
	if err := docindex.Build(ctx, store, docIdx, docsFrom, logger); err != nil {
		logger.Fatalf("build docindex: %v", err)
	}
	// Checkpoint watermark = the store's true current end sequence, NOT
	// idx.LatestSeq()/docIdx.LatestSeq() (the highest sequence among only
	// *matching* records). Using the per-type watermark meant a warm start
	// resumed from "last matching record + 1" and had to re-Scan every
	// non-matching record between there and the store's real end on every
	// restart -- on this store that was ~19,000 interleaved records costing
	// ~5s, most of the warm-start budget, for zero new signals. The store's
	// actual end is the correct "nothing new to see before this" watermark.
	if endSeq, err := store.LatestSequence(ctx); err != nil {
		logger.Printf("WARNING: get store latest sequence failed (%v); checkpointing at index watermark instead", err)
		saveCheckpoint(ckpt, idx, docIdx, max(idx.LatestSeq(), docIdx.LatestSeq()), logger)
	} else {
		saveCheckpoint(ckpt, idx, docIdx, endSeq, logger)
	}
	ready := signalindex.Tail(ctx, store, idx, *pollInterval, logger)
	<-ready
	docReady := docindex.Tail(ctx, store, docIdx, *pollInterval, logger)
	<-docReady

	// Periodic checkpoint sync: full-snapshot upsert every pollInterval,
	// cheap at this index size (thousands of rows, in-process SQLite,
	// single writer) -- simpler than tracking per-poll deltas, and it's a
	// disposable cache regardless (deleting it just costs one rebuild).
	go func() {
		t := time.NewTicker(*pollInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				endSeq, err := store.LatestSequence(ctx)
				if err != nil {
					logger.Printf("WARNING: get store latest sequence failed (%v); skipping this checkpoint sync", err)
					continue
				}
				saveCheckpoint(ckpt, idx, docIdx, endSeq, logger)
			}
		}
	}()
	cfg := apiserver.ServerConfig{
		Addr:         *addr,
		Index:        idx,
		DocIndex:     docIdx,
		Logger:       logger,
		APIKeys:      splitCSV(*apiKeys),
		ReadTimeout:  *readTimeout,
		WriteTimeout: *writeTimeout,
		MaxLimit:     *maxLimit,
	}
	// Optional IDUNA JWT auth — activated when IDUNA_JWKS_URL is set.
	if jwksURL := os.Getenv("IDUNA_JWKS_URL"); jwksURL != "" {
		v, err := idunaauth.NewVerifier(jwksURL)
		if err != nil {
			logger.Printf("WARNING: IDUNA JWT verifier init failed (%v); falling back to API key auth only", err)
		} else {
			cfg.IDUNAVerifier = v
			logger.Printf("IDUNA JWT auth enabled jwks_url=%s", jwksURL)
		}
	}
	// MySQL read model — use real MySQL when MYSQL_URL is set, SQLite otherwise.
	if mysqlURL := os.Getenv("MYSQL_URL"); mysqlURL != "" {
		db, err := sql.Open("mysql", mysqlURL+"?parseTime=true")
		if err != nil {
			logger.Printf("WARNING: MySQL open failed (%v); falling back to SQLite", err)
		} else if err := db.PingContext(ctx); err != nil {
			logger.Printf("WARNING: MySQL ping failed (%v); falling back to SQLite", err)
			db.Close()
		} else {
			cfg.MySQL = db
			defer db.Close()
			logger.Printf("MySQL connected: using real MySQL for governance-signals + eps + signals endpoints")
		}
	}
	if cfg.MySQL == nil {
		sqliteDB, err := openSQLiteReadModel(*storeRoot, logger)
		if err != nil {
			logger.Printf("WARNING: SQLite fallback failed (%v); governance-signals + signals endpoints will return 503", err)
		} else {
			cfg.MySQL = sqliteDB
			defer sqliteDB.Close()
			logger.Printf("SQLite read model ready (no MYSQL_URL set)")
		}
	}
	// Optional MongoDB entity graph — activated when MONGODB_URL is set.
	if mongoURL := os.Getenv("MONGODB_URL"); mongoURL != "" {
		mc, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURL))
		if err != nil {
			logger.Printf("WARNING: MongoDB connect failed (%v); entities endpoint disabled", err)
		} else if err := mc.Ping(ctx, nil); err != nil {
			logger.Printf("WARNING: MongoDB ping failed (%v); entities endpoint disabled", err)
			mc.Disconnect(ctx) //nolint:errcheck
		} else {
			cfg.Mongo = mc
			cfg.MongoDB = envOr("MONGODB_DB", "fatbaby")
			defer mc.Disconnect(ctx) //nolint:errcheck
			logger.Printf("MongoDB connected db=%s: entities endpoint enabled", cfg.MongoDB)
		}
	}
	// Co-occurrence store — seeded from signal index at startup (S126-11).
	coStore := cooccurrence.NewStore()
	summaries := idx.Summary()
	var allSignals []cooccurrence.TickerSignal
	for _, ts := range summaries {
		entries, ok := idx.ForTicker(ts.Ticker)
		if !ok {
			continue
		}
		for _, e := range entries {
			allSignals = append(allSignals, cooccurrence.TickerSignal{Ticker: ts.Ticker, Timestamp: e.Timestamp})
		}
	}
	coStore.SeedFromSignals(allSignals)
	cfg.CoOccurrence = coStore
	logger.Printf("co-occurrence store seeded tickers=%d edges=%d", len(summaries), coStore.EdgeCount())

	srv := apiserver.New(cfg)
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	logger.Printf("signal API ready addr=%s tickers=%d signals=%d latest_seq=%d scan_took=%s", *addr, len(idx.Summary()), idx.Depth(), idx.LatestSeq(), scanTook)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Fatalf("listen: %v", err)
	}
}

// openSQLiteReadModel opens (or creates) a SQLite read model at
// <storeRoot>/../signalapi.db and runs the MySQL migrations through the
// regex translator. It is the zero-ops fallback when MYSQL_URL is not set.
func openSQLiteReadModel(storeRoot string, logger *log.Logger) (*sql.DB, error) {
	// Place the SQLite file one level above the event store root so it
	// survives event store rotations.
	dbPath := filepath.Join(filepath.Dir(storeRoot), "signalapi.db")
	db, err := store.OpenSQLite(dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	migrationsDir := filepath.Join("migrations", "mysql")
	if err := store.RunSQLiteMigrations(db, migrationsDir); err != nil {
		db.Close()
		return nil, fmt.Errorf("run sqlite migrations: %w", err)
	}
	logger.Printf("SQLite read model at %s", dbPath)
	return db, nil
}

// saveCheckpoint upserts both indexes' current full state to the checkpoint
// under a single shared watermark (the store's true end sequence -- see the
// call site's comment for why this is not idx.LatestSeq()/docIdx.LatestSeq()).
func saveCheckpoint(ckpt *indexcheckpoint.DB, idx *signalindex.Index, docIdx *docindex.Index, watermark uint64, logger *log.Logger) {
	if err := ckpt.SaveSignals(idx.AllEntries(), watermark); err != nil {
		logger.Printf("WARNING: checkpoint signals save failed: %v", err)
	}
	if err := ckpt.SaveDocs(docIdx.AllSummaries(), watermark); err != nil {
		logger.Printf("WARNING: checkpoint docs save failed: %v", err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}
