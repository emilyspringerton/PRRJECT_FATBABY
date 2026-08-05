// cmd/pr-reaction-watcher tracks how a ticker's real stock price moves after
// a press release or SEC filing is published -- founder, live: "we need to
// start tracking price action/reaction when a pr is released and then at
// certain time intervals after the release so we can start tracking how
// certain companies respond to news." Sample points, founder-confirmed:
// T+0 (release), +15min, +1h, EOD (real trading-day close), +1 trading day,
// +3 trading days -- see internal/prreaction for the market-calendar-aware
// target-time math and the Yahoo Finance quote fetch.
//
// Reads two existing, real event stores (secwatch's filing_discovered,
// prwatch's pr_discovered -- both already carry a ticker directly, no new
// ticker-map needed) and writes its own: var/pr-reaction/
// price_reaction_scheduled + price_reaction_sample events.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
	identitypkg "github.com/example/prrject-fatbaby/internal/identity"
	"github.com/example/prrject-fatbaby/internal/prreaction"
	"github.com/example/prrject-fatbaby/prwatch"
	"github.com/example/prrject-fatbaby/secwatch"
)

func main() {
	secwatchRoot := flag.String("secwatch-store", filepath.Join("var", "secwatch"), "secwatch event store root (source: filing_discovered)")
	prwatchRoot := flag.String("prwatch-store", filepath.Join("var", "prwatch"), "prwatch event store root (source: pr_discovered)")
	outRoot := flag.String("store", filepath.Join("var", "pr-reaction"), "this service's own event store root")
	pollInterval := flag.Duration("poll-interval", 60*time.Second, "polling interval")
	horizon := flag.Duration("horizon", 96*time.Hour, "stop actively retrying a release once it's this old (t3d's own worst case is ~5 calendar days; a generous buffer past that)")
	flag.Parse()

	logger := log.New(os.Stdout, "", log.LstdFlags|log.LUTC)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	secStore, err := eventstore.NewFileStore(*secwatchRoot)
	if err != nil {
		logger.Fatalf("open secwatch store: %v", err)
	}
	defer secStore.Close()
	prStore, err := eventstore.NewFileStore(*prwatchRoot)
	if err != nil {
		logger.Fatalf("open prwatch store: %v", err)
	}
	defer prStore.Close()
	outStore, err := eventstore.NewFileStore(*outRoot)
	if err != nil {
		logger.Fatalf("open pr-reaction store: %v", err)
	}
	defer outStore.Close()

	cursorDir := filepath.Join(*outRoot, ".cursors")
	if err := os.MkdirAll(cursorDir, 0o755); err != nil {
		logger.Fatalf("mkdir cursor dir: %v", err)
	}
	filingCursorPath := filepath.Join(cursorDir, "secwatch")
	prCursorPath := filepath.Join(cursorDir, "prwatch")

	tracker := newTracker(logger)
	if err := tracker.loadFromStore(ctx, outStore); err != nil {
		logger.Fatalf("rebuild tracker state from own store: %v", err)
	}
	logger.Printf("pr-reaction-watcher starting scheduled=%d poll_interval=%s horizon=%s", len(tracker.releases), *pollInterval, *horizon)

	httpClient := &http.Client{Timeout: 20 * time.Second}
	filingCursor := loadCursor(filingCursorPath)
	prCursor := loadCursor(prCursorPath)

	for {
		newFilingCursor, err := ingestFilings(ctx, secStore, outStore, tracker, filingCursor, *horizon, logger)
		if err != nil {
			logger.Printf("ingest filings: %v", err)
		} else if newFilingCursor != filingCursor {
			filingCursor = newFilingCursor
			saveCursor(filingCursorPath, filingCursor)
		}

		newPRCursor, err := ingestPressReleases(ctx, prStore, outStore, tracker, prCursor, *horizon, logger)
		if err != nil {
			logger.Printf("ingest press releases: %v", err)
		} else if newPRCursor != prCursor {
			prCursor = newPRCursor
			saveCursor(prCursorPath, prCursor)
		}

		sampleDue(ctx, httpClient, outStore, tracker, *horizon, logger)

		select {
		case <-ctx.Done():
			logger.Printf("shutting down")
			return
		case <-time.After(*pollInterval):
		}
	}
}

// releaseState is the in-memory record of one tracked release: what's
// scheduled, and which offsets have a real sample already.
type releaseState struct {
	ScheduledEvent prreaction.ScheduledEvent
	Sampled        map[prreaction.Offset]bool
	BaselinePrice  float64 // t0's own price, once sampled
	HasBaseline    bool
}

type tracker struct {
	logger   *log.Logger
	releases map[string]*releaseState // keyed by identity
}

func newTracker(logger *log.Logger) *tracker {
	return &tracker{logger: logger, releases: map[string]*releaseState{}}
}

// loadFromStore rebuilds in-memory state from this service's own real event
// history -- same "the store is the source of truth, not a side file"
// approach the rest of this pipeline already uses.
func (tr *tracker) loadFromStore(ctx context.Context, store eventstore.EventStore) error {
	return store.Scan(ctx, 1, func(rec eventstore.Record) error {
		switch rec.Event.Type {
		case "price_reaction_scheduled":
			var ev prreaction.ScheduledEvent
			if err := json.Unmarshal(rec.Event.Data, &ev); err != nil {
				return nil
			}
			tr.releases[ev.Identity] = &releaseState{ScheduledEvent: ev, Sampled: map[prreaction.Offset]bool{}}
		case "price_reaction_sample":
			var ev prreaction.SampleEvent
			if err := json.Unmarshal(rec.Event.Data, &ev); err != nil {
				return nil
			}
			rs, ok := tr.releases[ev.Identity]
			if !ok {
				return nil // scheduled event should always precede its samples; skip defensively if not
			}
			rs.Sampled[ev.Offset] = true
			if ev.Offset == prreaction.OffsetT0 {
				rs.BaselinePrice = ev.Price
				rs.HasBaseline = true
			}
		}
		return nil
	})
}

func ingestFilings(ctx context.Context, secStore, outStore eventstore.EventStore, tr *tracker, fromSeq string, horizon time.Duration, logger *log.Logger) (string, error) {
	cursor := parseCursor(fromSeq)
	var lastSeq uint64
	recs, err := secStore.ReadFrom(ctx, cursor, 512)
	if err != nil {
		return fromSeq, err
	}
	for _, r := range recs {
		lastSeq = r.Sequence
		if r.Event.Type != "filing_discovered" {
			continue
		}
		var ev secwatch.FilingDiscoveredEvent
		if err := json.Unmarshal(r.Event.Data, &ev); err != nil {
			continue
		}
		if ev.Ticker == "" {
			continue
		}
		identity := secwatch.FilingIdentity(ev.CIK, ev.AccessionNumber)
		if _, exists := tr.releases[identity]; exists {
			continue
		}
		releaseTime := parseFilingTime(ev)
		// Cold-start/backfill guard: a first run against a real, years-deep
		// store would otherwise schedule (and never sample) millions of
		// ancient filings. Cursor still advances past these -- only the
		// scheduling is skipped.
		if time.Since(releaseTime) > horizon {
			continue
		}
		scheduled := prreaction.ScheduledEvent{
			Identity: identity, Ticker: ev.Ticker, Kind: "filing",
			Form: ev.EffectiveForm(), ReleaseTime: releaseTime,
		}
		if err := appendScheduled(ctx, outStore, scheduled); err != nil {
			logger.Printf("append scheduled (filing) identity=%s err=%v", identity, err)
			continue
		}
		tr.releases[identity] = &releaseState{ScheduledEvent: scheduled, Sampled: map[prreaction.Offset]bool{}}
		logger.Printf("scheduled kind=filing identity=%s ticker=%s form=%s release_time=%s", identity, ev.Ticker, ev.EffectiveForm(), releaseTime.Format(time.RFC3339))
	}
	if lastSeq == 0 {
		return fromSeq, nil
	}
	return strconv.FormatUint(lastSeq+1, 10), nil
}

func ingestPressReleases(ctx context.Context, prStore, outStore eventstore.EventStore, tr *tracker, fromSeq string, horizon time.Duration, logger *log.Logger) (string, error) {
	cursor := parseCursor(fromSeq)
	var lastSeq uint64
	recs, err := prStore.ReadFrom(ctx, cursor, 512)
	if err != nil {
		return fromSeq, err
	}
	for _, r := range recs {
		lastSeq = r.Sequence
		if r.Event.Type != "pr_discovered" {
			continue
		}
		var ev prwatch.PressReleaseDiscovered
		if err := json.Unmarshal(r.Event.Data, &ev); err != nil {
			continue
		}
		ticker := tickerFor(ev.Identity)
		if ticker == "" {
			continue
		}
		identity := r.Event.PartitionKey
		if identity == "" {
			identity = ev.URL
		}
		if _, exists := tr.releases[identity]; exists {
			continue
		}
		releaseTime := ev.DiscoveredAt
		if releaseTime.IsZero() {
			releaseTime = time.Now().UTC()
		}
		// Same cold-start/backfill guard as ingestFilings -- see its own comment.
		if time.Since(releaseTime) > horizon {
			continue
		}
		scheduled := prreaction.ScheduledEvent{
			Identity: identity, Ticker: ticker, Kind: "press_release",
			Headline: ev.Headline, ReleaseTime: releaseTime,
		}
		if err := appendScheduled(ctx, outStore, scheduled); err != nil {
			logger.Printf("append scheduled (press_release) identity=%s err=%v", identity, err)
			continue
		}
		tr.releases[identity] = &releaseState{ScheduledEvent: scheduled, Sampled: map[prreaction.Offset]bool{}}
		logger.Printf("scheduled kind=press_release identity=%s ticker=%s release_time=%s", identity, ticker, releaseTime.Format(time.RFC3339))
	}
	if lastSeq == 0 {
		return fromSeq, nil
	}
	return strconv.FormatUint(lastSeq+1, 10), nil
}

func tickerFor(id identitypkg.DiscoveryIdentity) string {
	if id.PrimaryTicker != nil {
		return id.PrimaryTicker.Symbol
	}
	return ""
}

func appendScheduled(ctx context.Context, store eventstore.EventStore, ev prreaction.ScheduledEvent) error {
	payload, _ := json.Marshal(ev)
	_, err := store.Append(ctx, eventstore.Event{
		ID: "price_reaction_scheduled:" + ev.Identity, Type: "price_reaction_scheduled",
		PartitionKey: ev.Identity, Source: "pr-reaction-watcher", Data: payload,
	})
	return err
}

// sampleDue checks every tracked, not-yet-fully-sampled release and fetches
// any offset whose target time has passed. Best-effort: a fetch failure for
// one (identity, offset) pair just tries again next tick, same convention
// the rest of this pipeline already uses for transient errors.
func sampleDue(ctx context.Context, client *http.Client, outStore eventstore.EventStore, tr *tracker, horizon time.Duration, logger *log.Logger) {
	now := time.Now().UTC()
	for identity, rs := range tr.releases {
		if len(rs.Sampled) >= len(prreaction.AllOffsets) {
			continue // fully sampled, nothing left to do
		}
		age := now.Sub(rs.ScheduledEvent.ReleaseTime)
		if age > horizon {
			continue // real, if unlikely, incomplete tracking (fetch failures every tick for days) -- abandoned rather than retried forever
		}
		for _, offset := range prreaction.AllOffsets {
			if rs.Sampled[offset] {
				continue
			}
			target := prreaction.TargetTime(rs.ScheduledEvent.ReleaseTime, offset)
			if now.Before(target) {
				continue // not due yet
			}
			bar, ok, err := prreaction.QuoteAt(ctx, client, rs.ScheduledEvent.Ticker, offset, target)
			if err != nil {
				logger.Printf("quote fetch failed identity=%s ticker=%s offset=%s err=%v", identity, rs.ScheduledEvent.Ticker, offset, err)
				continue
			}
			if !ok {
				continue // no usable bar yet; try again next tick
			}
			sample := prreaction.SampleEvent{
				Identity: identity, Ticker: rs.ScheduledEvent.Ticker, Offset: offset,
				TargetTime: target, SampledAt: now, Price: bar.Close,
			}
			if offset == prreaction.OffsetT0 {
				rs.BaselinePrice = bar.Close
				rs.HasBaseline = true
			} else if rs.HasBaseline && rs.BaselinePrice != 0 {
				sample.BaselinePrice = rs.BaselinePrice
				sample.PctChange = (bar.Close - rs.BaselinePrice) / rs.BaselinePrice * 100
			}
			payload, _ := json.Marshal(sample)
			if _, err := outStore.Append(ctx, eventstore.Event{
				ID: fmt.Sprintf("price_reaction_sample:%s:%s", identity, offset), Type: "price_reaction_sample",
				PartitionKey: identity, Source: "pr-reaction-watcher", Data: payload,
			}); err != nil {
				logger.Printf("append sample failed identity=%s offset=%s err=%v", identity, offset, err)
				continue
			}
			rs.Sampled[offset] = true
			logger.Printf("sampled identity=%s ticker=%s offset=%s price=%.4f pct_change=%.3f", identity, rs.ScheduledEvent.Ticker, offset, bar.Close, sample.PctChange)
		}
	}
}

func parseFilingTime(ev secwatch.FilingDiscoveredEvent) time.Time {
	if ev.AcceptanceDateTime != "" {
		if t, err := time.Parse(time.RFC3339, ev.AcceptanceDateTime); err == nil {
			return t.UTC()
		}
		if t, err := time.Parse("20060102150405", ev.AcceptanceDateTime); err == nil {
			return t.UTC()
		}
	}
	if ev.FilingDate != "" {
		if t, err := time.Parse("2006-01-02", ev.FilingDate); err == nil {
			return t.UTC()
		}
	}
	return time.Now().UTC()
}

func parseCursor(s string) uint64 {
	if s == "" {
		return 1
	}
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 1
	}
	return n
}

func loadCursor(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

func saveCursor(path, val string) {
	_ = os.WriteFile(path, []byte(val), 0o644)
}
