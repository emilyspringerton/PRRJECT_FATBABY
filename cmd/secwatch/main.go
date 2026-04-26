package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/example/prrject-fatbaby/secwatch"
)

func main() {
	var (
		watchlistPath = flag.String("watchlist", filepath.Join("config", "watchlist.json"), "watchlist config path")
		storeRoot     = flag.String("store", filepath.Join("var", "secwatch"), "event store root")
		dryRun        = flag.Bool("dry-run", false, "discover but do not persist events")
		ua            = flag.String("user-agent", "prrject-fatbaby-secwatch/0.1 (contact: secops@example.com)", "SEC-compliant user-agent with contact info")
		rateRPS       = flag.Float64("rate-rps", 2.0, "global request rate limit")
		burst         = flag.Int("burst", 3, "global request burst")
		timeout       = flag.Duration("timeout", 25*time.Second, "request timeout")
		retries       = flag.Int("retries", 4, "max retries")
		concurrency   = flag.Int("concurrency", 2, "bounded worker concurrency")
	)
	flag.Parse()

	logger := log.New(os.Stdout, "", log.LstdFlags|log.LUTC)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	client := secwatch.NewClient(secwatch.ClientConfig{
		UserAgent:    *ua,
		Timeout:      *timeout,
		RateLimitRPS: *rateRPS,
		RateBurst:    *burst,
		MaxRetries:   *retries,
	})

	_, err := secwatch.RunDiscovery(ctx, secwatch.RunnerConfig{
		WatchlistPath: *watchlistPath,
		StoreRoot:     *storeRoot,
		DryRun:        *dryRun,
		Concurrency:   *concurrency,
		Logger:        logger,
		Client:        client,
	})
	if err != nil {
		logger.Fatalf("secwatch run failed: %v", err)
	}
}
