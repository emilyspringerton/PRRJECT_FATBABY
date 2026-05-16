package prwatch

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/example/prrject-fatbaby/internal/processor"
)

// BodyFetchedEvent is the payload written to the body store for every
// successfully crawled press release.
type BodyFetchedEvent struct {
	// Mirror the discovery fields so consumers need only this event.
	PRDiscoveryID string `json:"pr_discovery_id"`
	Headline      string `json:"headline"`
	Company       string `json:"company,omitempty"`
	URL           string `json:"url"`
	PublishedAt   string `json:"published_at,omitempty"`

	// The cleaned plain-text body of the full press release page.
	Body string `json:"body"`

	FetchedAt string `json:"fetched_at"`
}

// BodyFailedEvent is written when the fetch or clean step fails, so the
// crawler can skip already-attempted URLs on restart instead of hammering
// a bad endpoint.
type BodyFailedEvent struct {
	PRDiscoveryID string `json:"pr_discovery_id"`
	URL           string `json:"url"`
	Error         string `json:"error"`
	FailedAt      string `json:"failed_at"`
}

// CrawlerConfig wires together the two stores and all tunable knobs.
type CrawlerConfig struct {
	// DiscoveryStore is read-only from the crawler's perspective.
	// It is the store that prwatch runner writes pr_discovered events into.
	DiscoveryStore eventstore.EventStore

	// BodyStore is where the crawler writes pr_body_fetched (and
	// pr_body_failed) events.  It lives in a separate root dir
	// (e.g. var/prwatch-body) so the two streams can be tailed,
	// backed-up, and retained independently.
	BodyStore eventstore.EventStore

	// Workers is the number of concurrent fetch goroutines.
	// Defaults to 4.  Keep this low — prnewswire will rate-limit you.
	Workers int

	// PollInterval is how long the crawler sleeps after draining the
	// discovery store before checking for new pr_discovered events.
	// Defaults to 15s.
	PollInterval time.Duration

	// UserAgent is sent on every HTTP request.
	UserAgent string

	// MaxDocBytes caps the raw HTML size fetched per URL.
	// Defaults to 4 MiB.
	MaxDocBytes int64

	// Now returns the current time.  Override in tests.
	Now func() time.Time

	Logger Logger
}

// RunBodyCrawler is the top-level blocking call.  It tails the discovery
// store for pr_discovered events, fans them into a worker pool that fetches
// and cleans the full body, and writes pr_body_fetched events into the body
// store.  It mirrors the shape of processor.Run exactly.
func RunBodyCrawler(ctx context.Context, cfg CrawlerConfig) error {
	if cfg.Workers <= 0 {
		cfg.Workers = 4
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 15 * time.Second
	}
	if cfg.MaxDocBytes <= 0 {
		cfg.MaxDocBytes = 4 << 20
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	if cfg.Logger == nil {
		cfg.Logger = &stdoutLogger{}
	}

	// Build the seen set once at startup.  After that we track newly seen
	// IDs in memory so the hot path does not re-scan the body store on
	// every batch.
	seen, err := loadBodySeenIDs(ctx, cfg.BodyStore)
	if err != nil {
		return fmt.Errorf("load body seen ids: %w", err)
	}
	var seenMu sync.Mutex

	lastSeq := uint64(1)
	cfg.Logger.Printf("body_crawler start from_sequence=%d workers=%d poll_interval=%s", lastSeq, cfg.Workers, cfg.PollInterval)

	for {
		recs, err := cfg.DiscoveryStore.ReadFrom(ctx, lastSeq, 512)
		if err != nil {
			return fmt.Errorf("read discovery store: %w", err)
		}

		if len(recs) > 0 {
			lastSeq = recs[len(recs)-1].Sequence + 1
			crawlBatch(ctx, cfg, recs, seen, &seenMu)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(cfg.PollInterval):
		}
	}
}

// crawlBatch fans out one batch of records to the worker pool.  Only
// pr_discovered events that have not already been crawled are enqueued.
func crawlBatch(
	ctx context.Context,
	cfg CrawlerConfig,
	recs []eventstore.Record,
	seen map[string]struct{},
	seenMu *sync.Mutex,
) {
	type job struct {
		discovery DiscoveredEvent
		prID      string
	}

	jobs := make(chan job)
	var wg sync.WaitGroup
	for i := 0; i < cfg.Workers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := range jobs {
				cfg.Logger.Printf("body_crawler worker=%d fetch_start pr_id=%s url=%s", id, j.prID, j.discovery.URL)
				if err := crawlOne(ctx, cfg, j.prID, j.discovery); err != nil {
					cfg.Logger.Printf("body_crawler worker=%d fetch_failed pr_id=%s err=%v", id, j.prID, err)
				} else {
					cfg.Logger.Printf("body_crawler worker=%d fetch_done pr_id=%s", id, j.prID)
				}
				seenMu.Lock()
				seen[j.prID] = struct{}{}
				seenMu.Unlock()
			}
		}(i + 1)
	}

	matched := 0
	for _, r := range recs {
		if r.Event.Type != "pr_discovered" {
			continue
		}
		var ev DiscoveredEvent
		if err := json.Unmarshal(r.Event.Data, &ev); err != nil {
			cfg.Logger.Printf("body_crawler skip sequence=%d reason=unmarshal_failed err=%v", r.Sequence, err)
			continue
		}
		prID := r.Event.AggregateKey
		if prID == "" {
			prID = ev.URL
		}

		seenMu.Lock()
		_, already := seen[prID]
		seenMu.Unlock()
		if already {
			continue
		}

		matched++
		jobs <- job{discovery: ev, prID: prID}
	}
	close(jobs)
	wg.Wait()

	cfg.Logger.Printf("body_crawler batch_complete total=%d enqueued=%d", len(recs), matched)
}

// crawlOne fetches, cleans, and persists a single press release body.
func crawlOne(ctx context.Context, cfg CrawlerConfig, prID string, ev DiscoveredEvent) error {
	now := cfg.Now()
	nowStr := now.Format(time.RFC3339Nano)

	clean, err := processor.FetchAndCleanText(ctx, ev.URL, cfg.UserAgent, cfg.MaxDocBytes)
	if err != nil {
		// Persist a failure event so we don't retry endlessly on restart.
		payload, _ := json.Marshal(BodyFailedEvent{
			PRDiscoveryID: prID,
			URL:           ev.URL,
			Error:         err.Error(),
			FailedAt:      nowStr,
		})
		_, _ = cfg.BodyStore.Append(ctx, eventstore.Event{
			ID:           "pr_body_failed:" + prID,
			Type:         "pr_body_failed",
			OccurredAt:   now,
			AggregateKey: prID,
			Source:       "prwatch_crawler",
			Data:         payload,
		})
		return err
	}

	payload, _ := json.Marshal(BodyFetchedEvent{
		PRDiscoveryID: prID,
		Headline:      ev.Headline,
		Company:       ev.Company,
		URL:           ev.URL,
		PublishedAt:   ev.PublishedAt,
		Body:          clean,
		FetchedAt:     nowStr,
	})
	_, err = cfg.BodyStore.Append(ctx, eventstore.Event{
		ID:           "pr_body_fetched:" + prID,
		Type:         "pr_body_fetched",
		OccurredAt:   now,
		AggregateKey: prID,
		Source:       "prwatch_crawler",
		Data:         payload,
	})
	return err
}

// loadBodySeenIDs reads the body store once at startup and returns the set
// of AggregateKeys for events that have already been processed (both
// pr_body_fetched and pr_body_failed count as "seen" to avoid re-fetching
// known-bad URLs on every restart).
func loadBodySeenIDs(ctx context.Context, store eventstore.EventStore) (map[string]struct{}, error) {
	seen := map[string]struct{}{}
	from := uint64(1)
	for {
		recs, err := store.ReadFrom(ctx, from, 512)
		if err != nil {
			return nil, fmt.Errorf("read body store for dedupe: %w", err)
		}
		if len(recs) == 0 {
			return seen, nil
		}
		for _, rec := range recs {
			switch rec.Event.Type {
			case "pr_body_fetched", "pr_body_failed":
				if rec.Event.AggregateKey != "" {
					seen[rec.Event.AggregateKey] = struct{}{}
				}
			}
		}
		from = recs[len(recs)-1].Sequence + 1
	}
}

type stdoutLogger struct{}

func (s *stdoutLogger) Printf(format string, args ...any) {
	fmt.Printf(format+"\n", args...)
}
