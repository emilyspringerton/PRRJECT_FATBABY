// cmd/dividend-watcher polls the prwatch-body event store for pr_body_fetched events,
// classifies each press release as a dividend announcement, and emits
// dividend_cut / dividend_raise / special_dividend signals to the entity-graph
// signal store.
//
// Flags:
//
//	-discovery-store  var/prwatch        (pr_discovered events for ticker mapping)
//	-body-store       var/prwatch-body   (pr_body_fetched events)
//	-graph-dir        var/entity-graph   (writes signals.ndjson)
//	-out-dir          var/dividends      (writes dividends.ndjson)
//	-cursor           var/dividend-watcher/.cursor
//	-poll-interval    30s
//	-one-shot
//	-dry-run
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/example/prrject-fatbaby/internal/dividend"
	"github.com/example/prrject-fatbaby/internal/entitygraph"
	"github.com/example/prrject-fatbaby/internal/prspam"
	"github.com/example/prrject-fatbaby/prwatch"
)

func main() {
	discoveryRoot := flag.String("discovery-store", filepath.Join("var", "prwatch"), "prwatch discovery store root")
	bodyRoot := flag.String("body-store", filepath.Join("var", "prwatch-body"), "prwatch body store root")
	graphDir := flag.String("graph-dir", filepath.Join("var", "entity-graph"), "entity-graph data directory")
	outDir := flag.String("out-dir", filepath.Join("var", "dividends"), "output directory for dividends.ndjson")
	cursorPath := flag.String("cursor", filepath.Join("var", "dividend-watcher", ".cursor"), "cursor file")
	pollInterval := flag.Duration("poll-interval", 30*time.Second, "poll interval")
	oneShot := flag.Bool("one-shot", false, "exit after one batch")
	dryRun := flag.Bool("dry-run", false, "log findings without writing")
	batchSize := flag.Int("batch-size", 256, "max events per batch")
	flag.Parse()

	logger := log.New(os.Stdout, "dividend-watcher ", log.LstdFlags|log.LUTC)

	for _, dir := range []string{*outDir, *graphDir, filepath.Dir(*cursorPath)} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			logger.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	discoveryStore, err := eventstore.NewFileStore(*discoveryRoot)
	if err != nil {
		logger.Fatalf("open discovery store: %v", err)
	}
	defer discoveryStore.Close()

	bodyStore, err := eventstore.NewFileStore(*bodyRoot)
	if err != nil {
		logger.Fatalf("open body store: %v", err)
	}
	defer bodyStore.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	cfg := batchConfig{
		graphDir:   *graphDir,
		outDir:     *outDir,
		cursorPath: *cursorPath,
		batchSize:  *batchSize,
		dryRun:     *dryRun,
	}

	// Build ticker map from discovery store.
	tickerMap := buildTickerMap(ctx, discoveryStore, logger)
	logger.Printf("ticker map: %d entries", len(tickerMap))

	cursor := loadCursor(*cursorPath)
	logger.Printf("starting from cursor=%d", cursor)

	for {
		cursor = runBatch(ctx, bodyStore, tickerMap, cursor, logger, cfg)
		if *oneShot {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(*pollInterval):
		}
	}
}

type batchConfig struct {
	graphDir   string
	outDir     string
	cursorPath string
	batchSize  int
	dryRun     bool
}

func runBatch(ctx context.Context, bodyStore eventstore.EventStore, tickerMap map[string]string, cursor uint64, logger *log.Logger, cfg batchConfig) uint64 {
	recs, err := bodyStore.ReadFrom(ctx, cursor+1, cfg.batchSize)
	if err != nil {
		logger.Printf("read body store: %v", err)
		return cursor
	}
	if len(recs) == 0 {
		return cursor
	}

	var dividends []dividend.DividendEvent
	var signals []entitygraph.Signal

	for _, rec := range recs {
		if ctx.Err() != nil {
			break
		}
		if rec.Event.Type != "pr_body_fetched" {
			continue
		}
		var body prwatch.BodyFetchedEvent
		if err := json.Unmarshal(rec.Event.Data, &body); err != nil {
			continue
		}
		if body.Body == "" {
			continue
		}

		ticker := tickerMap[body.PRDiscoveryID]
		if ticker == "" {
			continue
		}

		// S170-07 follow-up: dividend.Classify's own core regex only requires
		// "dividend"/"distribution" to appear anywhere in headline+body -- a
		// securities-litigation-alert press release about a dividend-paying
		// company (a BDC's own regular distribution is exactly the kind of
		// detail that ends up quoted in the litigation copy) trips that just
		// as easily as a real announcement. Confirmed live: 14 of 20 records
		// in var/dividends/dividends.ndjson were this exact contamination,
		// including at least one real "raise" event fabricated from an
		// INVESTOR ALERT. Same prspam filter guidance-watcher uses for the
		// identical reason (S170-07), applied here before classification.
		if prspam.IsLitigationAlertHeadline(body.Headline) {
			continue
		}

		// S170-231: a real, non-spam release's own page can embed PRNewswire's
		// "Also from this source" widget teasing a DIFFERENT press release from
		// the same company -- confirmed live, a "Target Announces Voting Results
		// from Annual Meeting of Shareholders" release fabricated a dividend
		// "raise" signal from a teased, unrelated "Target Corporation Increases
		// Quarterly Dividend" article a few paragraphs down the same page.
		// Truncate before classifying, not after -- same reasoning as the
		// litigation-alert filter just above.
		classifyBody := prspam.StripRelatedArticles(body.Body)

		ev := dividend.Classify(body.Headline, classifyBody)
		if ev == nil || ev.EventType == dividend.EventRegular {
			continue
		}
		ev.Ticker = ticker
		ev.SourceURL = body.URL
		ev.PublishedAt = body.PublishedAt

		sigs := dividend.Score(ev)

		if cfg.dryRun {
			logger.Printf("[dry-run] seq=%d ticker=%q event=%s amount=%.4f signals=%d headline=%q",
				rec.Sequence, ticker, ev.EventType, ev.AmountPerShare, len(sigs), body.Headline)
			continue
		}

		dividends = append(dividends, *ev)
		signals = append(signals, sigs...)
		logger.Printf("seq=%d ticker=%q event=%s amount=%.4f signals=%d", rec.Sequence, ticker, ev.EventType, ev.AmountPerShare, len(sigs))
	}

	if !cfg.dryRun {
		if len(dividends) > 0 {
			if err := appendDividends(cfg.outDir, dividends); err != nil {
				logger.Printf("write dividends: %v", err)
			}
			if err := entitygraph.WriteSignals(cfg.graphDir, signals); err != nil {
				logger.Printf("write signals: %v", err)
			}
		}
		newCursor := recs[len(recs)-1].Sequence
		saveCursor(cfg.cursorPath, newCursor)
		return newCursor
	}
	return cursor
}

// ── Persistence ───────────────────────────────────────────────────────────────

func appendDividends(dir string, evs []dividend.DividendEvent) error {
	p := filepath.Join(dir, "dividends.ndjson")
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for i := range evs {
		if err := enc.Encode(&evs[i]); err != nil {
			return err
		}
	}
	return nil
}

func loadCursor(path string) uint64 {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	var v uint64
	fmt.Sscan(string(b), &v)
	return v
}

func saveCursor(path string, seq uint64) {
	_ = os.WriteFile(path, []byte(fmt.Sprintf("%d", seq)), 0o644)
}

// buildTickerMap reads pr_discovered events and maps PRDiscoveryID → primary ticker.
func buildTickerMap(ctx context.Context, store eventstore.EventStore, logger *log.Logger) map[string]string {
	m := map[string]string{}
	err := store.Scan(ctx, 1, func(rec eventstore.Record) error {
		if rec.Event.Type != "pr_discovered" {
			return nil
		}
		var ev prwatch.PressReleaseDiscovered
		if err := json.Unmarshal(rec.Event.Data, &ev); err != nil {
			return nil
		}
		if ev.Identity.PrimaryTicker != nil && ev.Identity.PrimaryTicker.Symbol != "" {
			if id, ok := ev.Metadata["id"]; ok {
				m[id] = ev.Identity.PrimaryTicker.Symbol
			}
		}
		return nil
	})
	if err != nil && logger != nil {
		logger.Printf("ticker map scan error: %v", err)
	}
	return m
}
