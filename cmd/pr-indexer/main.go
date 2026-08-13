// pr-indexer polls the prwatch-body event store for pr_body_fetched events
// and persists a source_document_persisted event (SourceType "press_release")
// into the shared secwatch-rooted event store newssite's docindex reads from.
//
// Real gap found and fixed 2026-08-13 (founder real-time: "press releases page
// is not updating" / "i should see all tickered press releases" / newssite
// "publishes the same ... governance story over and over"): NOTHING in this
// pipeline ever wrote a source_document_persisted event for a press release.
// eps-processor/dividend-watcher/guidance-watcher/buyback-watcher each read
// pr_body_fetched independently for their own specialized signal extraction
// (oracle cases, dividend/guidance/buyback classification) but none of them
// fed a general-purpose SourceDocument back into docindex -- confirmed by
// grepping every one of them for "source_document_persisted" (zero hits) and
// by the on-disk event stores themselves: every source_document_persisted
// event on this box lives in var/secwatch, none in var/prwatch-body, and the
// most recent one predates today. The handful of "press releases" newssite's
// /wire page WAS showing were leftover mislabeled SEC filings from a
// different, already-fixed historical bug (see internal/processor/worker.go's
// sourceTypeForForm doc comment) -- not real press releases at all, which is
// why the page looked stuck repeating old governance stories.
//
// Mirrors guidance-watcher's own structure exactly (same event sources, same
// cursor pattern, same ticker map built from pr_discovered events) -- see
// that file's own doc comment.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/example/prrject-fatbaby/internal/prspam"
	"github.com/example/prrject-fatbaby/pkg/intelligence"
	"github.com/example/prrject-fatbaby/prwatch"
)

func main() {
	discoveryRoot := flag.String("discovery-store", filepath.Join("var", "prwatch"), "prwatch discovery event store root")
	bodyRoot := flag.String("body-store", filepath.Join("var", "prwatch-body"), "prwatch body event store root")
	outRoot := flag.String("out-store", filepath.Join("var", "secwatch"), "event store root to write source_document_persisted into (must match newssite's -store)")
	pollInterval := flag.Duration("poll-interval", 30*time.Second, "poll interval")
	cursorPath := flag.String("cursor", filepath.Join("var", "pr-indexer", ".cursor"), "cursor file")
	batchSize := flag.Int("batch-size", 256, "events per poll")
	oneShot := flag.Bool("one-shot", false, "process one batch and exit")
	flag.Parse()

	logger := log.New(os.Stdout, "pr-indexer ", log.LstdFlags|log.LUTC)

	if err := os.MkdirAll(filepath.Dir(*cursorPath), 0o755); err != nil {
		logger.Fatalf("mkdir cursor dir: %v", err)
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

	outStore, err := eventstore.NewFileStore(*outRoot)
	if err != nil {
		logger.Fatalf("open out store: %v", err)
	}
	defer outStore.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	logger.Printf("starting poll_interval=%s body_store=%s out_store=%s", *pollInterval, *bodyRoot, *outRoot)

	cursor := loadCursor(*cursorPath, logger)

	for {
		tickerByID := buildTickerMap(ctx, discoveryStore, logger)

		cursor = runBatch(ctx, bodyStore, outStore, tickerByID, logger, batchConfig{
			cursorPath: *cursorPath,
			batchSize:  *batchSize,
			cursor:     cursor,
		})

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
	cursorPath string
	batchSize  int
	cursor     uint64
}

func runBatch(ctx context.Context, bodyStore, outStore eventstore.EventStore, tickerByID map[string]string, logger *log.Logger, cfg batchConfig) uint64 {
	recs, err := bodyStore.ReadFrom(ctx, cfg.cursor, cfg.batchSize)
	if err != nil {
		logger.Printf("read batch cursor=%d err=%v", cfg.cursor, err)
		return cfg.cursor
	}
	if len(recs) == 0 {
		return cfg.cursor
	}

	published := 0
	skipped := 0

	for _, rec := range recs {
		if rec.Event.Type != "pr_body_fetched" {
			continue
		}
		var ev prwatch.BodyFetchedEvent
		if err := json.Unmarshal(rec.Event.Data, &ev); err != nil {
			continue
		}
		ticker := tickerByID[ev.PRDiscoveryID]
		if ticker == "" {
			// No (EXCHANGE:TICKER) mention found anywhere in the body during
			// discovery -- can't index without a ticker (docindex.Ingest
			// itself drops empty-ticker docs). A real, separate gap: press
			// releases about companies whose ticker mention landed outside
			// prwatch's own regex/precheck scan window aren't indexable at
			// all today, not something this pass fixes.
			skipped++
			continue
		}
		if len(ev.Body) < 200 {
			skipped++
			continue
		}
		if prspam.IsLitigationAlertHeadline(ev.Headline) {
			skipped++
			continue
		}

		doc := intelligence.SourceDocument{
			Identity:         "press_release:" + ev.PRDiscoveryID,
			Ticker:           ticker,
			SourceType:       "press_release",
			Form:             "",
			DocumentURL:      ev.URL,
			CleanedText:      ev.Body,
			CleanedCharCount: len(ev.Body),
			FilingDate:       "", // press releases have no SEC filing date; docindex falls back to PersistedAt for recency
			PersistedAt:      time.Now().UTC(),
		}
		payload, err := json.Marshal(doc)
		if err != nil {
			logger.Printf("marshal source document pr=%s err=%v", ev.PRDiscoveryID, err)
			continue
		}
		if _, err := outStore.Append(ctx, eventstore.Event{
			ID:           "source_document_persisted:" + doc.Identity,
			Type:         "source_document_persisted",
			PartitionKey: doc.Identity,
			Source:       "pr-indexer",
			Data:         payload,
		}); err != nil {
			logger.Printf("append source document pr=%s err=%v", ev.PRDiscoveryID, err)
			continue
		}
		published++
		logger.Printf("indexed ticker=%s pr=%s headline=%q", ticker, ev.PRDiscoveryID, ev.Headline)
	}

	logger.Printf("batch done published=%d skipped=%d", published, skipped)

	newCursor := recs[len(recs)-1].Sequence + 1
	writeCursor(cfg.cursorPath, newCursor, logger)
	return newCursor
}

func loadCursor(path string, logger *log.Logger) uint64 {
	b, err := os.ReadFile(path)
	if err != nil {
		return 1
	}
	var v uint64
	if err := json.Unmarshal(b, &v); err != nil {
		return 1
	}
	logger.Printf("cursor loaded seq=%d from %s", v, path)
	return v
}

func writeCursor(path string, seq uint64, logger *log.Logger) {
	b, err := json.Marshal(seq)
	if err != nil {
		return
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		logger.Printf("write cursor: %v", err)
	}
}

// buildTickerMap mirrors guidance-watcher's own helper exactly (same join
// key: ev.Metadata["id"], which is pr.ID -- the same value pr_body_fetched
// calls PRDiscoveryID).
func buildTickerMap(ctx context.Context, store eventstore.EventStore, logger *log.Logger) map[string]string {
	m := make(map[string]string)
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
