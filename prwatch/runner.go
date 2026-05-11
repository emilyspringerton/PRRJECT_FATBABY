package prwatch

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
)

type Logger interface {
	Printf(format string, args ...any)
}

type RunnerConfig struct {
	StoreRoot string
	DryRun    bool
	Now       func() time.Time
	Logger    Logger
	Client    *Client
}

type Summary struct {
	SeenSkipped int
	Discovered  int
}

type DiscoveredEvent struct {
	Headline     string `json:"headline"`
	Company      string `json:"company,omitempty"`
	URL          string `json:"url"`
	PublishedAt  string `json:"published_at,omitempty"`
	DiscoveredAt string `json:"discovered_at"`
}

func RunDiscovery(ctx context.Context, cfg RunnerConfig) (Summary, error) {
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	if cfg.Client == nil {
		cfg.Client = NewClient(ClientConfig{})
	}
	disc, err := cfg.Client.Discover(ctx)
	if err != nil {
		return Summary{}, err
	}
	store, err := eventstore.NewFileStore(cfg.StoreRoot)
	if err != nil {
		return Summary{}, fmt.Errorf("open event store: %w", err)
	}
	defer store.Close()

	seen, err := LoadSeenIDs(ctx, store)
	if err != nil {
		return Summary{}, err
	}
	s := Summary{}
	for _, pr := range disc {
		if _, ok := seen[pr.ID]; ok {
			s.SeenSkipped++
			continue
		}
		s.Discovered++
		if cfg.DryRun {
			continue
		}
		ev := eventstore.Event{
			ID:           "pr_discovered:" + pr.ID,
			Type:         "pr_discovered",
			OccurredAt:   cfg.Now(),
			AggregateKey: pr.ID,
			Source:       "prnewswire",
			Data:         mustJSON(eventData(pr, cfg.Now())),
		}
		if _, err := store.Append(ctx, ev); err != nil {
			return s, fmt.Errorf("append event %s: %w", pr.ID, err)
		}
		seen[pr.ID] = struct{}{}
	}
	if cfg.Logger != nil {
		cfg.Logger.Printf("prwatch summary discovered=%d seen=%d dry_run=%t", s.Discovered, s.SeenSkipped, cfg.DryRun)
	}
	return s, nil
}

func eventData(pr PRDiscovery, now time.Time) DiscoveredEvent {
	e := DiscoveredEvent{Headline: pr.Headline, Company: pr.Company, URL: pr.URL, DiscoveredAt: now.UTC().Format(time.RFC3339Nano)}
	if !pr.Timestamp.IsZero() {
		e.PublishedAt = pr.Timestamp.UTC().Format(time.RFC3339Nano)
	}
	return e
}

func LoadSeenIDs(ctx context.Context, store eventstore.EventStore) (map[string]struct{}, error) {
	seen := map[string]struct{}{}
	from := uint64(1)
	for {
		recs, err := store.ReadFrom(ctx, from, 512)
		if err != nil {
			return nil, fmt.Errorf("read events for dedupe: %w", err)
		}
		if len(recs) == 0 {
			return seen, nil
		}
		for _, rec := range recs {
			if rec.Event.Type != "pr_discovered" {
				continue
			}
			if rec.Event.AggregateKey != "" {
				seen[rec.Event.AggregateKey] = struct{}{}
				continue
			}
			var e DiscoveredEvent
			if err := json.Unmarshal(rec.Event.Data, &e); err == nil && e.URL != "" {
				seen[e.URL] = struct{}{}
			}
		}
		from = recs[len(recs)-1].Sequence + 1
	}
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
