package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

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
