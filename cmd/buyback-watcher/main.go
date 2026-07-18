// cmd/buyback-watcher polls the prwatch-body event store for pr_body_fetched events,
// classifies each press release as a share repurchase announcement, and emits
// buyback_authorization / buyback_suspension signals to the entity-graph store.
//
// Flags:
//   -discovery-store  var/prwatch
//   -body-store       var/prwatch-body
//   -graph-dir        var/entity-graph
//   -out-dir          var/buybacks
//   -cursor           var/buyback-watcher/.cursor
//   -poll-interval    30s
//   -one-shot
//   -dry-run
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
	"github.com/example/prrject-fatbaby/internal/buyback"
	"github.com/example/prrject-fatbaby/internal/entitygraph"
	"github.com/example/prrject-fatbaby/prwatch"
)

func main() {
	discoveryRoot := flag.String("discovery-store", filepath.Join("var", "prwatch"), "prwatch discovery store root")
	bodyRoot := flag.String("body-store", filepath.Join("var", "prwatch-body"), "prwatch body store root")
	graphDir := flag.String("graph-dir", filepath.Join("var", "entity-graph"), "entity-graph data directory")
	outDir := flag.String("out-dir", filepath.Join("var", "buybacks"), "output directory for buybacks.ndjson")
	cursorPath := flag.String("cursor", filepath.Join("var", "buyback-watcher", ".cursor"), "cursor file")
	pollInterval := flag.Duration("poll-interval", 30*time.Second, "poll interval")
	oneShot := flag.Bool("one-shot", false, "exit after one batch")
	dryRun := flag.Bool("dry-run", false, "log findings without writing")
	batchSize := flag.Int("batch-size", 256, "max events per batch")
	flag.Parse()

	logger := log.New(os.Stdout, "buyback-watcher ", log.LstdFlags|log.LUTC)

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

	tickerMap := buildTickerMap(ctx, discoveryStore, logger)
	logger.Printf("ticker map: %d entries", len(tickerMap))

	cursor := loadCursor(*cursorPath)
	logger.Printf("starting from cursor=%d", cursor)

	cfg := batchConfig{graphDir: *graphDir, outDir: *outDir, cursorPath: *cursorPath, batchSize: *batchSize, dryRun: *dryRun}

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
	graphDir, outDir, cursorPath string
	batchSize                    int
	dryRun                       bool
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

	var events []buyback.BuybackEvent
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

		ev := buyback.Classify(body.Headline, body.Body)
		if ev == nil || ev.EventType == buyback.EventUpdate {
			continue
		}
		ev.Ticker = ticker
		ev.SourceURL = body.URL
		ev.PublishedAt = body.PublishedAt

		sigs := buyback.Score(ev)

		if cfg.dryRun {
			logger.Printf("[dry-run] seq=%d ticker=%q event=%s amount=%.0fM signals=%d",
				rec.Sequence, ticker, ev.EventType, ev.AuthorizedUSD/1e6, len(sigs))
			continue
		}

		events = append(events, *ev)
		signals = append(signals, sigs...)
		logger.Printf("seq=%d ticker=%q event=%s amount_usd=%.0f", rec.Sequence, ticker, ev.EventType, ev.AuthorizedUSD)
	}

	if !cfg.dryRun {
		if len(events) > 0 {
			if err := appendEvents(cfg.outDir, events); err != nil {
				logger.Printf("write buybacks: %v", err)
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

func appendEvents(dir string, evs []buyback.BuybackEvent) error {
	f, err := os.OpenFile(filepath.Join(dir, "buybacks.ndjson"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
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
