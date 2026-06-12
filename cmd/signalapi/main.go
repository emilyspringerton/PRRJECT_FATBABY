package main

import (
	"context"
	"database/sql"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/example/prrject-fatbaby/internal/apiserver"
	"github.com/example/prrject-fatbaby/internal/idunaauth"
	"github.com/example/prrject-fatbaby/internal/signalindex"
)

func main() {
	storeRoot := flag.String("store", "var/secwatch", "")
	addr := flag.String("addr", ":9091", "")
	apiKeys := flag.String("api-keys", "", "")
	pollInterval := flag.Duration("poll-interval", 2*time.Second, "")
	maxLimit := flag.Int("max-limit", 100, "")
	readTimeout := flag.Duration("read-timeout", 10*time.Second, "")
	writeTimeout := flag.Duration("write-timeout", 30*time.Second, "")
	flag.Parse()
	logger := log.New(os.Stdout, "", log.LstdFlags|log.LUTC)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	store, err := eventstore.NewFileStore(*storeRoot)
	if err != nil {
		logger.Fatalf("open store: %v", err)
	}
	defer store.Close()
	idx := signalindex.NewIndex()
	scanStart := time.Now()
	if err := signalindex.Build(ctx, store, idx, logger); err != nil {
		logger.Fatalf("build index: %v", err)
	}
	scanTook := time.Since(scanStart)
	ready := signalindex.Tail(ctx, store, idx, *pollInterval, logger)
	<-ready
	cfg := apiserver.ServerConfig{
		Addr:         *addr,
		Index:        idx,
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
	// Optional MySQL read model — activated when MYSQL_URL is set.
	if mysqlURL := os.Getenv("MYSQL_URL"); mysqlURL != "" {
		db, err := sql.Open("mysql", mysqlURL+"?parseTime=true")
		if err != nil {
			logger.Printf("WARNING: MySQL open failed (%v); governance-signals + eps endpoints disabled", err)
		} else if err := db.PingContext(ctx); err != nil {
			logger.Printf("WARNING: MySQL ping failed (%v); governance-signals + eps endpoints disabled", err)
			db.Close()
		} else {
			cfg.MySQL = db
			defer db.Close()
			logger.Printf("MySQL connected: governance-signals + eps endpoints enabled")
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
