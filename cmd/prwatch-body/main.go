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

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/example/prrject-fatbaby/prwatch"
)

func main() {
	var (
		discoveryRoot = flag.String("discovery-store", filepath.Join("var", "prwatch"), "root dir of the pr_discovered event store (written by cmd/prwatch)")
		bodyRoot      = flag.String("body-store", filepath.Join("var", "prwatch-body"), "root dir of the pr_body_fetched event store (written by this command)")
		ua            = flag.String("user-agent", "prrject-fatbaby-prwatch-body/0.1 (contact: secops@example.com)", "HTTP user agent")
		workers       = flag.Int("workers", 4, "concurrent fetch goroutines")
		pollInterval  = flag.Duration("poll-interval", 15*time.Second, "sleep between discovery store tails")
		maxDocBytes   = flag.Int64("max-doc-bytes", 4<<20, "max raw HTML bytes fetched per URL")
	)
	flag.Parse()

	logger := log.New(os.Stdout, "", log.LstdFlags|log.LUTC)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	discoveryStore, err := eventstore.NewFileStore(*discoveryRoot)
	if err != nil {
		logger.Fatalf("open discovery store %s: %v", *discoveryRoot, err)
	}
	defer func() {
		if err := discoveryStore.Close(); err != nil {
			logger.Printf("close discovery store: %v", err)
		}
	}()

	bodyStore, err := eventstore.NewFileStore(*bodyRoot)
	if err != nil {
		logger.Fatalf("open body store %s: %v", *bodyRoot, err)
	}
	defer func() {
		if err := bodyStore.Close(); err != nil {
			logger.Printf("close body store: %v", err)
		}
	}()

	type logAdapter struct{ l *log.Logger }
	la := &logAdapter{l: logger}

	if err := prwatch.RunBodyCrawler(ctx, prwatch.CrawlerConfig{
		DiscoveryStore: discoveryStore,
		BodyStore:      bodyStore,
		Workers:        *workers,
		PollInterval:   *pollInterval,
		UserAgent:      *ua,
		MaxDocBytes:    *maxDocBytes,
		Logger:         *la,
	}); err != nil && err != context.Canceled {
		logger.Fatalf("body crawler exited: %v", err)
	}
}

// logAdapter satisfies prwatch.Logger using the stdlib logger.
type logAdapter struct{ l *log.Logger }

func (a *logAdapter) Printf(format string, args ...any) {
	a.l.Printf(format, args...)
}
