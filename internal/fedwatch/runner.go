package fedwatch

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

// FOMCPressDiscovered is the event payload for one newly-seen Fed
// monetary-policy press release.
type FOMCPressDiscovered struct {
	URL          string    `json:"url"`
	Title        string    `json:"title"`
	Description  string    `json:"description"`
	Category     string    `json:"category"`
	PublishedAt  string    `json:"published_at,omitempty"`
	DiscoveredAt time.Time `json:"discovered_at"`
}

// RunDiscovery polls the Fed's monetary-policy RSS feed once and appends
// one fomc_press_discovered event per not-previously-seen item. Same
// discover -> load-seen -> dedupe -> append shape as prwatch.RunDiscovery
// (S165-03's own "same shape as prwatch" instruction) -- eventstore is
// the dedup ledger, not an in-memory set that resets every run.
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
			ID:           "fomc_press_discovered:" + pr.ID,
			Type:         "fomc_press_discovered",
			OccurredAt:   cfg.Now(),
			PartitionKey: pr.ID,
			Source:       "federalreserve.gov",
			Data:         mustJSON(eventData(pr, cfg.Now())),
		}
		if _, err := store.Append(ctx, ev); err != nil {
			return s, fmt.Errorf("append event %s: %w", pr.ID, err)
		}
		seen[pr.ID] = struct{}{}
	}
	if cfg.Logger != nil {
		cfg.Logger.Printf("fedwatch summary discovered=%d seen=%d dry_run=%t", s.Discovered, s.SeenSkipped, cfg.DryRun)
	}
	return s, nil
}

func eventData(pr PressRelease, now time.Time) FOMCPressDiscovered {
	e := FOMCPressDiscovered{
		URL:          pr.URL,
		Title:        pr.Title,
		Description:  pr.Description,
		Category:     pr.Category,
		DiscoveredAt: now.UTC(),
	}
	if !pr.PublishedAt.IsZero() {
		e.PublishedAt = pr.PublishedAt.Format(time.RFC3339Nano)
	}
	return e
}

// LoadSeenIDs walks the whole event store once, same "PartitionKey first,
// fall back to decoding the payload's own identity field" shape as
// prwatch.LoadSeenIDs -- PartitionKey is the fast path for every event
// this package itself ever wrote; the decode fallback exists only for
// robustness against a hand-edited or foreign-written event lacking one.
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
			if rec.Event.Type != "fomc_press_discovered" {
				continue
			}
			if rec.Event.PartitionKey != "" {
				seen[rec.Event.PartitionKey] = struct{}{}
				continue
			}
			var e FOMCPressDiscovered
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
