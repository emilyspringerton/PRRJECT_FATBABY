package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/example/prrject-fatbaby/prwatch"
	"github.com/example/prrject-fatbaby/secwatch"
)

func main() {
	var (
		storeRoot     = flag.String("store", filepath.Join("var", "prwatch"), "event store root")
		dryRun        = flag.Bool("dry-run", false, "discover but do not persist events")
		ua            = flag.String("user-agent", "prrject-fatbaby-prwatch/0.1 (contact: secops@example.com)", "scraper user-agent")
		timeout       = flag.Duration("timeout", 20*time.Second, "request timeout")
		pollInterval  = flag.Duration("poll-interval", 15*time.Second, "interval between poll rounds")
		maxPolls      = flag.Int("max-polls", 0, "optional max poll rounds (0 = unbounded)")
		watchlistPath = flag.String("watchlist", filepath.Join("config", "watchlist.json"), "watchlist config path -- used only to mint SKULDMARK-25 IDs for regex-extracted tickers that happen to be on it")
	)
	flag.Parse()

	logger := log.New(os.Stdout, "", log.LstdFlags|log.LUTC)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	client := prwatch.NewClient(prwatch.ClientConfig{HTTPClient: &http.Client{Timeout: *timeout}, UserAgent: *ua})

	watchlistTickers := loadWatchlistTickers(*watchlistPath, logger)

	round := 0
	for {
		round++
		if _, err := prwatch.RunDiscovery(ctx, prwatch.RunnerConfig{StoreRoot: *storeRoot, DryRun: *dryRun, Logger: logger, Client: client, WatchlistTickers: watchlistTickers}); err != nil {
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

// loadWatchlistTickers reads the same watchlist.json secwatch uses and
// returns an upper-cased-ticker lookup of CIK+Exchange, for minting
// SKULDMARK-25 IDs on regex-extracted tickers that happen to be on the
// watchlist. A load failure is logged and non-fatal -- prwatch's own
// discovery doesn't depend on it, only ID minting does.
func loadWatchlistTickers(path string, logger *log.Logger) map[string]prwatch.WatchlistRef {
	wl, err := secwatch.LoadWatchlist(path)
	if err != nil {
		logger.Printf("prwatch: watchlist load failed (%v) -- SKULDMARK minting disabled this run", err)
		return nil
	}
	out := make(map[string]prwatch.WatchlistRef, len(wl.Entries))
	for _, e := range wl.Entries {
		out[strings.ToUpper(e.Ticker)] = prwatch.WatchlistRef{CIK: e.CIK, Exchange: e.Exchange}
	}
	return out
}
