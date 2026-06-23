// Package replay provides a signal replay engine for backtesting provider configurations.
//
// The ReplayEngine reads filing_discovered events from the event store within a time window,
// re-runs each through a Provider, and streams Signal results to an output channel.
//
// This allows comparing analysis quality across different providers (haiku vs archetype)
// against a fixed historical corpus without re-fetching SEC documents.
package replay

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/example/prrject-fatbaby/internal/processor"
	"github.com/example/prrject-fatbaby/pkg/intelligence"
	"github.com/example/prrject-fatbaby/secwatch"
)

// Stats summarizes a completed replay run.
type Stats struct {
	Total    int
	Success  int
	Failed   int
	Skipped  int           // events outside the time window or wrong type
	Duration time.Duration
}

// Result is one replayed signal or error.
type Result struct {
	EventID   string
	Ticker    string
	Form      string
	Signal    *intelligence.Signal
	Err       error
}

// ReplayEngine replays stored filing_discovered events through a Provider.
type ReplayEngine struct {
	Store    eventstore.EventStore
	Provider processor.Provider
	// Out receives each Result as it is produced. Caller must drain the channel.
	Out    chan Result
	Logger *log.Logger
}

// New creates a ReplayEngine with a buffered output channel of the given size.
func New(store eventstore.EventStore, provider processor.Provider, outBuf int, logger *log.Logger) *ReplayEngine {
	if logger == nil {
		logger = log.New(log.Writer(), "[replay] ", log.LstdFlags)
	}
	return &ReplayEngine{
		Store:    store,
		Provider: provider,
		Out:      make(chan Result, outBuf),
		Logger:   logger,
	}
}

// Replay reads all event store records and replays filing_discovered events whose
// OccurredAt is in [from, to] (inclusive). It sends each Result to e.Out and returns
// aggregate Stats. The Out channel is closed when Replay returns.
//
// The provider's AnalyzeText is called with the same text format the processor uses:
// "source_type=edgar\nform=<form>\n\n<ticker> accession=<accession>"
func (e *ReplayEngine) Replay(ctx context.Context, from, to time.Time) (Stats, error) {
	defer close(e.Out)

	started := time.Now()
	var stats Stats

	var seq uint64
	const batchSize = 100
	for {
		if ctx.Err() != nil {
			return stats, ctx.Err()
		}
		recs, err := e.Store.ReadFrom(ctx, seq, batchSize)
		if err != nil {
			return stats, fmt.Errorf("replay: ReadFrom seq=%d: %w", seq, err)
		}
		if len(recs) == 0 {
			break
		}
		for _, rec := range recs {
			seq = rec.Sequence + 1
			if rec.Event.Type != "filing_discovered" {
				stats.Skipped++
				continue
			}
			ts := rec.Event.OccurredAt
			if ts.Before(from) || ts.After(to) {
				stats.Skipped++
				continue
			}
			stats.Total++
			res := e.processRecord(ctx, rec)
			if res.Err != nil {
				stats.Failed++
			} else {
				stats.Success++
			}
			select {
			case e.Out <- res:
			case <-ctx.Done():
				stats.Duration = time.Since(started)
				return stats, ctx.Err()
			}
		}
	}

	stats.Duration = time.Since(started)
	return stats, nil
}

func (e *ReplayEngine) processRecord(ctx context.Context, rec eventstore.Record) Result {
	var filing secwatch.FilingDiscoveredEvent
	if err := json.Unmarshal(rec.Event.Data, &filing); err != nil {
		return Result{
			EventID: rec.Event.ID,
			Err:     fmt.Errorf("unmarshal filing_discovered: %w", err),
		}
	}
	ticker := filing.Ticker
	form := filing.EffectiveForm()
	accession := filing.AccessionNumber

	// Build the same text the processor uses.
	text := fmt.Sprintf("source_type=edgar\nform=%s\n\n%s accession=%s", form, ticker, accession)
	signal, err := e.Provider.AnalyzeText(ctx, text)
	if err != nil {
		e.Logger.Printf("replay: AnalyzeText ticker=%s accession=%s err=%v", ticker, accession, err)
		return Result{
			EventID: rec.Event.ID,
			Ticker:  ticker,
			Form:    form,
			Err:     err,
		}
	}
	return Result{
		EventID: rec.Event.ID,
		Ticker:  ticker,
		Form:    form,
		Signal:  signal,
	}
}
