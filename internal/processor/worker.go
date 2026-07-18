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

// seenSet tracks processed filing identities in memory so the hot path
// avoids full-store scans on every handleOne call (which would be O(n²)).
type seenSet struct {
	mu      sync.Mutex
	signals map[string]struct{}
	sources map[string]struct{}
}

func newSeenSet() *seenSet {
	return &seenSet{
		signals: make(map[string]struct{}),
		sources: make(map[string]struct{}),
	}
}

func (s *seenSet) hasSignal(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.signals[id]
	return ok
}

func (s *seenSet) markSignal(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.signals[id] = struct{}{}
}

func (s *seenSet) hasSource(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.sources[id]
	return ok
}

func (s *seenSet) markSource(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sources[id] = struct{}{}
}

// signal_failed retry TTL: 429 failures older than this are eligible for retry.
// Empty-URL and non-retryable errors stay blocked by checking the error field.
const failedRetryTTL = 30 * 24 * time.Hour

// loadSeenIdentities does a single O(n) scan of the store at startup to build
// the in-memory seen set, replacing per-filing O(n) calls inside handleOne.
// signal_failed events block retry only when (a) the error is a permanent failure
// (empty URL, non-HTTP) or (b) the failure is recent (< 30 days old). This lets
// 429-rate-limited filings be retried on the next processor restart after the
// EDGAR cooldown window has passed.
//
// Returns the highest sequence encountered so Run can resume its main loop
// from there instead of replaying the whole store a second time from
// sequence 1 (that redundant second replay, paged through ReadFrom 512
// records at a time with a poll-interval sleep between every page, used to
// make cold-start catch-up take up to ~an hour and was a direct contributor
// to processor's OOM kills — see docs/northstar/replay-fragility.md).
func loadSeenIdentities(ctx context.Context, store eventstore.EventStore, logger *log.Logger) (*seenSet, uint64) {
	seen := newSeenSet()
	var lastSeq uint64
	expiry := time.Now().UTC().Add(-failedRetryTTL)
	err := store.Scan(ctx, 1, func(r eventstore.Record) error {
		switch r.Event.Type {
		case "signal_generated":
			seen.signals[r.Event.PartitionKey] = struct{}{}
		case "signal_failed":
			// Only permanently block non-retryable failures. 429-based failures
			// older than failedRetryTTL are eligible for a fresh attempt.
			var meta map[string]string
			_ = json.Unmarshal(r.Event.Data, &meta)
			errMsg := meta["error"]
			isPermanent := strings.Contains(errMsg, "unsupported document URL") ||
				strings.Contains(errMsg, "status=404") ||
				strings.Contains(errMsg, "status=403")
			if isPermanent || r.AppendedAt.After(expiry) {
				seen.signals[r.Event.PartitionKey] = struct{}{}
			}
		case "source_document_persisted":
			seen.sources[r.Event.PartitionKey] = struct{}{}
		}
		if r.Sequence > lastSeq {
			lastSeq = r.Sequence
		}
		return nil
	})
	if err != nil && logger != nil {
		logger.Printf("processor startup: seen-set scan error: %v", err)
	}
	logger.Printf("processor startup: seen signals=%d sources=%d latest_seq=%d", len(seen.signals), len(seen.sources), lastSeq)
	return seen, lastSeq
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

	seen, seenLastSeq := loadSeenIdentities(ctx, cfg.Store, cfg.Logger)

	lastSeq := seenLastSeq + 1
	cfg.Logger.Printf("processor loop starting from_sequence=%d workers=%d poll_interval=%s", lastSeq, cfg.Workers, cfg.PollInterval)
	for {
		var recs []eventstore.Record
		if err := cfg.Store.Scan(ctx, lastSeq, func(rec eventstore.Record) error {
			recs = append(recs, rec)
			return nil
		}); err != nil {
			return fmt.Errorf("read events: %w", err)
		}
		if len(recs) > 0 {
			batchStart := recs[0].Sequence
			batchEnd := recs[len(recs)-1].Sequence
			cfg.Logger.Printf("processor batch read count=%d sequence_start=%d sequence_end=%d", len(recs), batchStart, batchEnd)
			lastSeq = batchEnd + 1
			processBatch(ctx, cfg, recs, seen)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(cfg.PollInterval):
		}
	}
}

func processBatch(ctx context.Context, cfg WorkerConfig, recs []eventstore.Record, seen *seenSet) {
	jobs := make(chan secwatch.FilingDiscoveredEvent)
	var wg sync.WaitGroup
	for i := 0; i < cfg.Workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for ev := range jobs {
				cfg.Logger.Printf("processor worker=%d handling filing_discovered ticker=%s cik=%s accession=%s", workerID, ev.Ticker, ev.CIK, ev.AccessionNumber)
				_ = handleOne(ctx, cfg, ev, seen)
			}
		}(i + 1)
	}
	matched := 0
	for _, r := range recs {
		cfg.Logger.Printf("processor saw event sequence=%d type=%s aggregate=%s source=%s", r.Sequence, r.Event.Type, r.Event.PartitionKey, r.Event.Source)
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

func handleOne(ctx context.Context, cfg WorkerConfig, filing secwatch.FilingDiscoveredEvent, seen *seenSet) error {
	form := filing.EffectiveForm()
	identity := secwatch.FilingIdentity(filing.CIK, filing.AccessionNumber)
	cfg.Logger.Printf("processor handle start identity=%s form=%s doc=%s", identity, form, filing.PrimaryDocument)
	if seen.hasSignal(identity) {
		cfg.Logger.Printf("processor handle skip identity=%s reason=signal_exists", identity)
		return nil
	}
	if !IsValidDocURL(filing.PrimaryDocument) {
		cfg.Logger.Printf("processor handle skip identity=%s reason=invalid_url doc=%q", identity, filing.PrimaryDocument)
		appendFailure(ctx, cfg.Store, filing, fmt.Errorf("unsupported document URL: %q", filing.PrimaryDocument))
		seen.markSignal(identity)
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
	if strings.Contains(strings.ToUpper(form), "8-K") {
		kind = "sec_8k"
	}
	if !seen.hasSource(identity) {
		if persistErr := persistSourceDocument(ctx, cfg.Store, filing, identity, kind, clean); persistErr != nil {
			cfg.Logger.Printf("processor source_document persist failed identity=%s err=%v", identity, persistErr)
		} else {
			seen.markSource(identity)
			cfg.Logger.Printf("processor source_document persisted identity=%s ticker=%s chars=%d", identity, filing.Ticker, len(clean))
		}
	} else {
		cfg.Logger.Printf("processor source_document already persisted identity=%s", identity)
	}
	signal, err := cfg.Provider.AnalyzeText(ctx, fmt.Sprintf("source_type=%s\nform=%s\n\n%s", kind, form, clean))
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
	// Stamp the original source document date so the projector can populate
	// source_published_at without re-parsing event store records.
	if signal.RawMetadata == nil {
		signal.RawMetadata = make(map[string]string)
	}
	if filing.FilingDate != "" {
		signal.RawMetadata["source_published_at"] = filing.FilingDate
		if signal.RawMetadata["filing_date"] == "" {
			signal.RawMetadata["filing_date"] = filing.FilingDate
		}
	}
	payload, _ := json.Marshal(signal)
	_, err = cfg.Store.Append(ctx, eventstore.Event{ID: "signal_generated:" + identity, Type: "signal_generated", PartitionKey: identity, Source: "processor", Data: payload})
	if err != nil {
		cfg.Logger.Printf("processor append failed identity=%s err=%v", identity, err)
		return err
	}
	seen.markSignal(identity)
	cfg.Logger.Printf("processor handle complete identity=%s signal_id=%s", identity, signal.ID)
	return nil
}

func appendFailure(ctx context.Context, store eventstore.EventStore, filing secwatch.FilingDiscoveredEvent, cause error) {
	payload, _ := json.Marshal(map[string]string{"ticker": filing.Ticker, "cik": filing.CIK, "accession_number": filing.AccessionNumber, "error": cause.Error()})
	_, _ = store.Append(ctx, eventstore.Event{ID: "signal_failed:" + secwatch.FilingIdentity(filing.CIK, filing.AccessionNumber), Type: "signal_failed", PartitionKey: secwatch.FilingIdentity(filing.CIK, filing.AccessionNumber), Source: "processor", Data: payload})
}

var _ = intelligence.Signal{}
