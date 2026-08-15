// fed-watch polls the Federal Reserve's public monetary-policy RSS feed
// once and records any newly-seen press release into the event store --
// same "one-shot, run from a systemd timer" idiom as bond-watcher, not a
// long-running poll loop. The Fed doesn't publish on a fixed daily
// cadence (statements/minutes/announcements land irregularly around
// FOMC meetings), so this doesn't gate on marketcal.IsMarketDay the way
// bond-watcher does -- eventstore's own seen-ID dedup is what keeps
// re-runs cheap, not a day-of-week check.
//
// S165-03 (EMILY/BACKLOG.md), HQ-SPEC-AI-103-adjacent auto-generated-
// articles Phase 3.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/example/prrject-fatbaby/internal/fedwatch"
)

func main() {
	storeRoot := flag.String("store", "var/secwatch", "eventstore root")
	dryRun := flag.Bool("dry-run", false, "fetch and print but do not write to the event store")
	flag.Parse()

	logger := log.New(os.Stdout, "fed-watch ", log.LstdFlags|log.LUTC)
	ctx := context.Background()

	client := fedwatch.NewClient(fedwatch.ClientConfig{HTTPClient: &http.Client{Timeout: 20 * time.Second}})

	summary, err := fedwatch.RunDiscovery(ctx, fedwatch.RunnerConfig{
		StoreRoot: *storeRoot,
		DryRun:    *dryRun,
		Client:    client,
		Logger:    logger,
	})
	if err != nil {
		logger.Fatalf("run discovery: %v", err)
	}

	now := time.Now().UTC()
	if next, ok := fedwatch.NextMeeting(now); ok {
		logger.Printf("next FOMC meeting: %s - %s", next.Start.Format("2006-01-02"), next.End.Format("2006-01-02"))
	} else {
		logger.Printf("WARNING: no tracked FOMC meeting on or after %s -- calendar.go needs a new year appended", now.Format("2006-01-02"))
	}

	logger.Printf("done: discovered=%d seen_skipped=%d", summary.Discovered, summary.SeenSkipped)
}
