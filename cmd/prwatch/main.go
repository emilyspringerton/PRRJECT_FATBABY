package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/example/prrject-fatbaby/prwatch"
)

func main() {
	var (
		storeRoot    = flag.String("store", filepath.Join("var", "prwatch"), "event store root")
		dryRun       = flag.Bool("dry-run", false, "discover but do not persist events")
		ua           = flag.String("user-agent", "prrject-fatbaby-prwatch/0.1 (contact: secops@example.com)", "scraper user-agent")
		timeout      = flag.Duration("timeout", 20*time.Second, "request timeout")
		pollInterval = flag.Duration("poll-interval", 15*time.Second, "interval between poll rounds")
		maxPolls     = flag.Int("max-polls", 0, "optional max poll rounds (0 = unbounded)")
	)
	flag.Parse()

	logger := log.New(os.Stdout, "", log.LstdFlags|log.LUTC)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	client := prwatch.NewClient(prwatch.ClientConfig{HTTPClient: &http.Client{Timeout: *timeout}, UserAgent: *ua})

	round := 0
	for {
		round++
		if _, err := prwatch.RunDiscovery(ctx, prwatch.RunnerConfig{StoreRoot: *storeRoot, DryRun: *dryRun, Logger: logger, Client: client}); err != nil {
			logger.Printf("prwatch run failed round=%d: %v", round, err)
		} else {
			logger.Printf("prwatch poll round=%d complete", round)
		}
		if *maxPolls > 0 && round >= *maxPolls {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(*pollInterval):
		}
	}
}
