package processor

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/example/prrject-fatbaby/pkg/intelligence"
	"github.com/example/prrject-fatbaby/secwatch"
)

type WorkerConfig struct {
	Store        eventstore.EventStore
	Provider     Provider
	Logger       *log.Logger
	Workers      int
	PollInterval time.Duration
	UserAgent    string
	MaxDocBytes  int64
}

func Run(ctx context.Context, cfg WorkerConfig) error {
	if cfg.Workers <= 0 {
		cfg.Workers = 4
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 15 * time.Second
	}
	if cfg.MaxDocBytes <= 0 {
		cfg.MaxDocBytes = 4 << 20
	}
	if cfg.Logger == nil {
		cfg.Logger = log.New(log.Writer(), "", log.LstdFlags|log.LUTC)
	}
	lastSeq := uint64(1)
	cfg.Logger.Printf("processor loop starting from_sequence=%d workers=%d poll_interval=%s", lastSeq, cfg.Workers, cfg.PollInterval)
	for {
		recs, err := cfg.Store.ReadFrom(ctx, lastSeq, 512)
		if err != nil {
			return fmt.Errorf("read events: %w", err)
		}
		if len(recs) > 0 {
			batchStart := recs[0].Sequence
			batchEnd := recs[len(recs)-1].Sequence
			cfg.Logger.Printf("processor batch read count=%d sequence_start=%d sequence_end=%d", len(recs), batchStart, batchEnd)
			lastSeq = batchEnd + 1
			processBatch(ctx, cfg, recs)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(cfg.PollInterval):
		}
	}
}

func processBatch(ctx context.Context, cfg WorkerConfig, recs []eventstore.Record) {
	jobs := make(chan secwatch.FilingDiscoveredEvent)
	var wg sync.WaitGroup
	for i := 0; i < cfg.Workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for ev := range jobs {
				cfg.Logger.Printf("processor worker=%d handling filing_discovered ticker=%s cik=%s accession=%s", workerID, ev.Ticker, ev.CIK, ev.AccessionNumber)
				_ = handleOne(ctx, cfg, ev)
			}
		}(i + 1)
	}
	matched := 0
	for _, r := range recs {
		cfg.Logger.Printf("processor saw event sequence=%d type=%s aggregate=%s source=%s", r.Sequence, r.Event.Type, r.Event.AggregateKey, r.Event.Source)
		if r.Event.Type != "filing_discovered" {
			continue
		}
		var ev secwatch.FilingDiscoveredEvent
		if err := json.Unmarshal(r.Event.Data, &ev); err != nil {
			cfg.Logger.Printf("processor skip sequence=%d reason=unmarshal_failed err=%v", r.Sequence, err)
			continue
		}
		matched++
		jobs <- ev
	}
	close(jobs)
	wg.Wait()
	cfg.Logger.Printf("processor batch complete total=%d matched_filing_discovered=%d skipped=%d", len(recs), matched, len(recs)-matched)
}

func handleOne(ctx context.Context, cfg WorkerConfig, filing secwatch.FilingDiscoveredEvent) error {
	identity := secwatch.FilingIdentity(filing.CIK, filing.AccessionNumber)
	cfg.Logger.Printf("processor handle start identity=%s form=%s doc=%s", identity, filing.Form, filing.PrimaryDocument)
	if signalExists(ctx, cfg.Store, identity) {
		cfg.Logger.Printf("processor handle skip identity=%s reason=signal_exists", identity)
		return nil
	}
	cfg.Logger.Printf("processor fetch start identity=%s", identity)
	clean, err := FetchAndCleanText(ctx, filing.PrimaryDocument, cfg.UserAgent, cfg.MaxDocBytes)
	if err != nil {
		cfg.Logger.Printf("processor fetch failed identity=%s err=%v", identity, err)
		appendFailure(ctx, cfg.Store, filing, err)
		return err
	}
	cfg.Logger.Printf("processor fetch complete identity=%s cleaned_chars=%d", identity, len(clean))
	kind := "press_release"
	if strings.Contains(strings.ToUpper(filing.Form), "8-K") {
		kind = "sec_8k"
	}
	signal, err := cfg.Provider.AnalyzeText(ctx, fmt.Sprintf("source_type=%s\nform=%s\n\n%s", kind, filing.Form, clean))
	if err != nil {
		cfg.Logger.Printf("processor analyze failed identity=%s err=%v", identity, err)
		appendFailure(ctx, cfg.Store, filing, err)
		return err
	}
	if signal.ID == "" {
		signal.ID = "signal:" + identity
	}
	if signal.Ticker == "" {
		signal.Ticker = filing.Ticker
	}
	if signal.Timestamp.IsZero() {
		signal.Timestamp = time.Now().UTC()
	}
	payload, _ := json.Marshal(signal)
	_, err = cfg.Store.Append(ctx, eventstore.Event{ID: "signal_generated:" + identity, Type: "signal_generated", AggregateKey: identity, Source: "processor", Data: payload})
	if err != nil {
		cfg.Logger.Printf("processor append failed identity=%s err=%v", identity, err)
		return err
	}
	cfg.Logger.Printf("processor handle complete identity=%s signal_id=%s", identity, signal.ID)
	return nil
}

func signalExists(ctx context.Context, store eventstore.EventStore, identity string) bool {
	from := uint64(1)
	for {
		recs, err := store.ReadFrom(ctx, from, 512)
		if err != nil || len(recs) == 0 {
			return false
		}
		for _, r := range recs {
			if r.Event.Type == "signal_generated" && r.Event.AggregateKey == identity {
				return true
			}
		}
		from = recs[len(recs)-1].Sequence + 1
	}
}

func appendFailure(ctx context.Context, store eventstore.EventStore, filing secwatch.FilingDiscoveredEvent, cause error) {
	payload, _ := json.Marshal(map[string]string{"ticker": filing.Ticker, "cik": filing.CIK, "accession_number": filing.AccessionNumber, "error": cause.Error()})
	_, _ = store.Append(ctx, eventstore.Event{ID: "signal_failed:" + secwatch.FilingIdentity(filing.CIK, filing.AccessionNumber), Type: "signal_failed", AggregateKey: secwatch.FilingIdentity(filing.CIK, filing.AccessionNumber), Source: "processor", Data: payload})
}

var _ = intelligence.Signal{}
