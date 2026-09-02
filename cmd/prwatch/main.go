package main

import (
	"context"
	"flag"
	"log"
	"math/rand"
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
		storeRoot       = flag.String("store", filepath.Join("var", "prwatch"), "event store root")
		dryRun          = flag.Bool("dry-run", false, "discover but do not persist events")
		ua              = flag.String("user-agent", "prrject-fatbaby-prwatch/0.1 (contact: secops@example.com)", "scraper user-agent")
		timeout         = flag.Duration("timeout", 20*time.Second, "request timeout")
		pollInterval    = flag.Duration("poll-interval", 15*time.Second, "fixed interval between poll rounds, used when -poll-interval-min/-max are unset (0 duration each, the default)")
		pollIntervalMin = flag.Duration("poll-interval-min", 0, "if set together with -poll-interval-max, each round's base wait is a fresh uniform random duration in [min,max] instead of the fixed -poll-interval. Founder real-time, 2026-09-02, for an eventual BusinessWire runner: \"have it crawl and then rest between 15 seconds and 1 minute\" -- recommended real values for that: -poll-interval-min=15s -poll-interval-max=1m -poll-jitter=10s")
		pollIntervalMax = flag.Duration("poll-interval-max", 0, "see -poll-interval-min")
		pollJitter      = flag.Duration("poll-jitter", 0, "additionally randomize each round's wait (fixed or ranged) by +/- this much (0 = no extra jitter, the old behavior). \"can we still add some basic jitter in the timing of the runner\" -- basic pacing variance, not active evasion (an occasional-decoy-click/wandering idea was raised and then explicitly paused by the founder in the same conversation, not built).")
		maxPolls        = flag.Int("max-polls", 0, "optional max poll rounds (0 = unbounded)")
		watchlistPath   = flag.String("watchlist", filepath.Join("config", "watchlist.json"), "watchlist config path -- used only to mint SKULDMARK-25 IDs for regex-extracted tickers that happen to be on it")
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
		case <-time.After(nextWait(*pollInterval, *pollIntervalMin, *pollIntervalMax, *pollJitter, rng)):
		}
	}
}

// rng is process-lifetime, not reseeded per round -- a fixed poll cadence is
// exactly the pattern that makes a scraper trivially fingerprintable by
// request timing; nextWait exists so real operators can turn that off
// without switching pollers or wiring in a whole new dependency.
var rng = rand.New(rand.NewSource(time.Now().UnixNano()))

// nextWait picks this round's real wait duration:
//  1. base = a fresh uniform random value in [intervalMin,intervalMax] when
//     both are set and intervalMax > intervalMin, else the fixed interval
//     (the old, exact behavior -- zero change for any caller that never
//     sets -poll-interval-min/-max).
//  2. then jitteredInterval adds up to +/- jitter on top of that base.
//
// Recommended real BusinessWire values (founder, 2026-09-02): interval=n/a,
// intervalMin=15s, intervalMax=1m, jitter=10s -- "crawl and then rest
// between 15 seconds and 1 minute +-10 and then crawl again."
func nextWait(interval, intervalMin, intervalMax, jitter time.Duration, rng *rand.Rand) time.Duration {
	base := interval
	if intervalMin > 0 && intervalMax > intervalMin {
		base = intervalMin + time.Duration(rng.Int63n(int64(intervalMax-intervalMin)+1))
	}
	return jitteredInterval(base, jitter, rng)
}

// jitteredInterval returns base +/- a uniform random amount up to jitter,
// clamped so it never returns a negative or zero wait (rand.Int63n panics on
// n<=0, and a zero-or-negative wait would busy-loop). jitter<=0 returns base
// unchanged -- the exact old fixed-cadence behavior, so -poll-jitter=0
// (the default) is a real no-op, not an approximation of one.
func jitteredInterval(base, jitter time.Duration, rng *rand.Rand) time.Duration {
	if jitter <= 0 {
		return base
	}
	delta := time.Duration(rng.Int63n(2*int64(jitter)+1)) - jitter // uniform in [-jitter, +jitter]
	out := base + delta
	if out <= 0 {
		return base
	}
	return out
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
