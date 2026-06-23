// stub-backfill re-analyzes source documents that were previously analyzed by the
// stub provider (summary="Stub analysis result.") using the haiku provider.
//
// It does NOT re-fetch from EDGAR — it reads the cached source_document_persisted
// events and re-runs haiku analysis, writing signal_generated_v2 events.
// The signalapi projector picks up _v2 events and upserts the signal.
//
// Usage:
//
//	ANTHROPIC_API_KEY=... go run ./cmd/stub-backfill -store var/secwatch
//
// Flags:
//
//	-store      event store root (default: var/secwatch)
//	-workers    parallel haiku workers (default: 2; respect rate limits)
//	-dry-run    scan and report without writing events
//	-limit      max documents to process (0 = all)
//	-ticker     only process this ticker (empty = all)
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/example/prrject-fatbaby/internal/processor"
	"github.com/example/prrject-fatbaby/pkg/intelligence"
)

func main() {
	storeRoot := flag.String("store", "var/secwatch", "event store root")
	workers := flag.Int("workers", 2, "parallel haiku workers")
	dryRun := flag.Bool("dry-run", false, "scan only, do not write events")
	limit := flag.Int("limit", 0, "max documents to backfill (0=all)")
	ticker := flag.String("ticker", "", "only process this ticker")
	flag.Parse()

	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		log.Fatalf("ANTHROPIC_API_KEY required")
	}
	if *dryRun {
		log.Printf("DRY RUN — no events will be written")
	}

	logger := log.New(os.Stdout, "stub-backfill ", log.LstdFlags|log.LUTC)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	store, err := eventstore.NewFileStore(*storeRoot)
	if err != nil {
		logger.Fatalf("open store: %v", err)
	}
	defer store.Close()

	// Phase 1: scan store to build maps:
	//   stubSignals: identity → Signal (where provider==stub)
	//   v2Signals: identity → true (already backfilled)
	//   sourceDocs: identity → SourceDocument
	logger.Printf("scanning event store...")
	stubSignals := make(map[string]*intelligence.Signal)
	v2Signals := make(map[string]bool)
	sourceDocs := make(map[string]*intelligence.SourceDocument)

	from := uint64(1)
	var totalScanned int
	for {
		recs, err := store.ReadFrom(ctx, from, 512)
		if err != nil || len(recs) == 0 {
			break
		}
		for _, r := range recs {
			totalScanned++
			switch r.Event.Type {
			case "signal_generated":
				var sig intelligence.Signal
				if err := json.Unmarshal(r.Event.Data, &sig); err == nil {
					if sig.RawMetadata["provider"] == "stub" {
						stubSignals[r.Event.PartitionKey] = &sig
					}
				}
			case "signal_generated_v2":
				v2Signals[r.Event.PartitionKey] = true
			case "source_document_persisted":
				var doc intelligence.SourceDocument
				if err := json.Unmarshal(r.Event.Data, &doc); err == nil {
					sourceDocs[r.Event.PartitionKey] = &doc
				}
			}
		}
		from = recs[len(recs)-1].Sequence + 1
	}
	logger.Printf("scan complete: total_events=%d stub_signals=%d source_docs=%d already_v2=%d",
		totalScanned, len(stubSignals), len(sourceDocs), len(v2Signals))

	// Build the backfill queue: stub signals with source docs, not already v2'd.
	type job struct {
		identity string
		doc      *intelligence.SourceDocument
		sig      *intelligence.Signal
	}
	var queue []job
	for identity, sig := range stubSignals {
		if v2Signals[identity] {
			continue
		}
		doc, ok := sourceDocs[identity]
		if !ok || doc.CleanedText == "" {
			continue
		}
		if *ticker != "" && !strings.EqualFold(sig.Ticker, *ticker) {
			continue
		}
		queue = append(queue, job{identity: identity, doc: doc, sig: sig})
	}
	logger.Printf("backfill queue: %d documents eligible", len(queue))
	if *limit > 0 && len(queue) > *limit {
		queue = queue[:*limit]
		logger.Printf("limiting to %d documents", *limit)
	}
	if *dryRun || len(queue) == 0 {
		logger.Printf("done (dry-run or nothing to do)")
		return
	}

	// Phase 2: re-analyze in parallel using haiku.
	prov := processor.NewHaikuProvider(apiKey)
	jobs := make(chan job, 32)
	var (
		wg       sync.WaitGroup
		okCount  atomic.Int64
		errCount atomic.Int64
	)
	for i := 0; i < *workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				if ctx.Err() != nil {
					return
				}
				prompt := fmt.Sprintf("source_type=%s\nform=%s\n\n%s",
					j.doc.SourceType, j.doc.Form, j.doc.CleanedText)
				sig, err := prov.AnalyzeText(ctx, prompt)
				if err != nil {
					logger.Printf("analyze failed identity=%s ticker=%s err=%v",
						j.identity, j.doc.Ticker, err)
					errCount.Add(1)
					continue
				}
				// Carry forward identity fields.
				sig.ID = "signal_v2:" + j.identity
				sig.Ticker = j.doc.Ticker
				if sig.Timestamp.IsZero() {
					sig.Timestamp = time.Now().UTC()
				}
				if sig.RawMetadata == nil {
					sig.RawMetadata = make(map[string]string)
				}
				sig.RawMetadata["filing_date"] = j.doc.FilingDate
				sig.RawMetadata["source_published_at"] = j.doc.FilingDate
				sig.RawMetadata["provider"] = "haiku"
				sig.RawMetadata["backfill_from_stub"] = "true"
				payload, _ := json.Marshal(sig)
				_, err = store.Append(ctx, eventstore.Event{
					ID:           "signal_generated_v2:" + j.identity,
					Type:         "signal_generated_v2",
					PartitionKey: j.identity,
					Source:       "stub-backfill",
					Data:         payload,
				})
				if err != nil {
					logger.Printf("append failed identity=%s err=%v", j.identity, err)
					errCount.Add(1)
					continue
				}
				n := okCount.Add(1)
				logger.Printf("backfilled identity=%s ticker=%s signal_type=%s importance=%d [%d/%d]",
					j.identity, sig.Ticker, sig.SignalType, sig.Importance, n, len(queue))
			}
		}()
	}
	for _, j := range queue {
		select {
		case <-ctx.Done():
			goto done
		case jobs <- j:
		}
	}
done:
	close(jobs)
	wg.Wait()
	logger.Printf("backfill complete: ok=%d errors=%d", okCount.Load(), errCount.Load())
}
