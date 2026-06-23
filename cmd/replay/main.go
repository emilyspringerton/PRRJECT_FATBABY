// replay — rerun historical filing_discovered events through a signal provider.
//
// Usage:
//
//	replay -from 2026-01-01 -to 2026-06-01 -provider haiku -output signals.jsonl
//
// Environment:
//
//	ANTHROPIC_API_KEY   — required when -provider=haiku
//	FATBABY_ROOT        — root of var/ directory tree (default: .)
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/example/prrject-fatbaby/internal/processor"
	"github.com/example/prrject-fatbaby/internal/replay"
	"github.com/example/prrject-fatbaby/pkg/intelligence"
)

const dateLayout = "2006-01-02"

func main() {
	fromStr := flag.String("from", "", "start date (YYYY-MM-DD, inclusive)")
	toStr := flag.String("to", "", "end date (YYYY-MM-DD, inclusive)")
	providerName := flag.String("provider", "haiku", "provider: haiku|stub")
	outputFile := flag.String("output", "", "output JSONL file (default: stdout)")
	storeDir := flag.String("store", "", "event store directory (default: $FATBABY_ROOT/var/secwatch)")
	verbose := flag.Bool("v", false, "verbose per-signal output")
	flag.Parse()

	logger := log.New(os.Stderr, "[replay] ", log.LstdFlags)

	if *fromStr == "" || *toStr == "" {
		logger.Fatal("-from and -to are required (format: 2006-01-02)")
	}
	from, err := time.ParseInLocation(dateLayout, *fromStr, time.UTC)
	if err != nil {
		logger.Fatalf("invalid -from: %v", err)
	}
	to, err := time.ParseInLocation(dateLayout, *toStr, time.UTC)
	if err != nil {
		logger.Fatalf("invalid -to: %v", err)
	}
	// to is end-of-day.
	to = to.Add(24*time.Hour - time.Nanosecond)

	// Resolve store directory.
	storePath := *storeDir
	if storePath == "" {
		root := os.Getenv("FATBABY_ROOT")
		if root == "" {
			root = "."
		}
		storePath = root + "/var/secwatch"
	}

	store, err := eventstore.NewFileStore(storePath)
	if err != nil {
		logger.Fatalf("open event store %s: %v", storePath, err)
	}
	defer store.Close()

	var prov processor.Provider
	switch *providerName {
	case "haiku":
		apiKey := os.Getenv("ANTHROPIC_API_KEY")
		if apiKey == "" {
			logger.Fatal("ANTHROPIC_API_KEY required for -provider=haiku")
		}
		prov = processor.NewHaikuProvider(apiKey)
	case "stub":
		prov = &stubProvider{}
	default:
		logger.Fatalf("unknown provider %q (supported: haiku, stub)", *providerName)
	}

	// Set up output writer.
	out := os.Stdout
	if *outputFile != "" {
		f, err := os.Create(*outputFile)
		if err != nil {
			logger.Fatalf("create output file: %v", err)
		}
		defer f.Close()
		out = f
	}

	enc := json.NewEncoder(out)
	engine := replay.New(store, prov, 64, logger)

	ctx := context.Background()
	var stats replay.Stats
	done := make(chan struct{})
	go func() {
		defer close(done)
		for res := range engine.Out {
			if res.Err != nil {
				if *verbose {
					fmt.Fprintf(os.Stderr, "FAIL  %-8s %-10s err=%v\n", res.Ticker, res.Form, res.Err)
				}
				continue
			}
			if *verbose {
				fmt.Fprintf(os.Stderr, "OK    %-8s %-10s type=%-12s importance=%d\n",
					res.Ticker, res.Form, res.Signal.SignalType, res.Signal.Importance)
			}
			enc.Encode(res.Signal)
		}
	}()

	stats, err = engine.Replay(ctx, from, to)
	<-done

	logger.Printf("replay complete: total=%d success=%d failed=%d skipped=%d duration=%s",
		stats.Total, stats.Success, stats.Failed, stats.Skipped, stats.Duration.Round(time.Millisecond))

	if err != nil {
		logger.Fatalf("replay error: %v", err)
	}
}

// stubProvider returns a fixed synthetic signal for testing without API calls.
type stubProvider struct{}

func (s *stubProvider) AnalyzeText(_ context.Context, _ string) (*intelligence.Signal, error) {
	return &intelligence.Signal{
		SignalType: "Other",
		Importance: 5,
		Sentiment:  0,
		Summary:    "stub replay signal",
	}, nil
}
