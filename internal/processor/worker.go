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
	lastSeq := uint64(1)
	for {
		recs, err := cfg.Store.ReadFrom(ctx, lastSeq, 512)
		if err != nil {
			return fmt.Errorf("read events: %w", err)
		}
		if len(recs) > 0 {
			lastSeq = recs[len(recs)-1].Sequence + 1
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
		go func() {
			defer wg.Done()
			for ev := range jobs {
				_ = handleOne(ctx, cfg, ev)
			}
		}()
	}
	for _, r := range recs {
		if r.Event.Type != "filing_discovered" {
			continue
		}
		var ev secwatch.FilingDiscoveredEvent
		if err := json.Unmarshal(r.Event.Data, &ev); err != nil {
			continue
		}
		jobs <- ev
	}
	close(jobs)
	wg.Wait()
}

func handleOne(ctx context.Context, cfg WorkerConfig, filing secwatch.FilingDiscoveredEvent) error {
	identity := secwatch.FilingIdentity(filing.CIK, filing.AccessionNumber)
	if signalExists(ctx, cfg.Store, identity) {
		return nil
	}
	clean, err := FetchAndCleanText(ctx, filing.PrimaryDocument, cfg.UserAgent, cfg.MaxDocBytes)
	if err != nil {
		appendFailure(ctx, cfg.Store, filing, err)
		return err
	}
	kind := "press_release"
	if strings.Contains(strings.ToUpper(filing.Form), "8-K") {
		kind = "sec_8k"
	}
	signal, err := cfg.Provider.AnalyzeText(ctx, fmt.Sprintf("source_type=%s\nform=%s\n\n%s", kind, filing.Form, clean))
	if err != nil {
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
	return err
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
