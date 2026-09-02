package prwatch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
	identitypkg "github.com/example/prrject-fatbaby/internal/identity"
	prid "github.com/example/prrject-fatbaby/internal/prwatch"
	"github.com/example/prrject-fatbaby/internal/skuldmarkid"
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
	// SourceName labels which wire service this run's events came from --
	// "prnewswire", "businesswire", etc. Threaded into both the
	// eventstore.Event.Source field and PressReleaseDiscovered.Source
	// (previously both hardcoded to the literal "prnewswire" regardless of
	// what Client actually pointed at -- a real, found bug: nothing in this
	// package ever supported more than one wire service's own labeling,
	// even though Client's own BaseURL was already configurable). Empty
	// defaults to "prnewswire", preserving every existing caller's exact
	// current behavior/output with zero config change required.
	SourceName string
	// WatchlistTickers maps an upper-cased ticker symbol to its known CIK +
	// exchange, keyed the same way as secwatch's own watchlist -- used to
	// mint a SKULDMARK-25 ID for regex-extracted tickers that happen to be
	// on the watchlist. Most press-release tickers won't be (prwatch covers
	// the whole PR Newswire firehose, not just the watchlist), and that's
	// expected: no CIK on file means no ID minted, not a guess. Optional;
	// nil means no minting happens here at all.
	WatchlistTickers map[string]WatchlistRef
}

// sourceName returns cfg.SourceName, defaulting to "prnewswire" -- see
// RunnerConfig.SourceName's own doc comment for why this default exists.
func (cfg RunnerConfig) sourceName() string {
	if cfg.SourceName == "" {
		return "prnewswire"
	}
	return cfg.SourceName
}

// WatchlistRef is the subset of a watchlist entry prwatch needs to mint a
// SKULDMARK-25 ID for a regex-extracted ticker it happens to match.
type WatchlistRef struct {
	CIK      string
	Exchange string
}

type Summary struct {
	SeenSkipped int
	Discovered  int
}

type PressReleaseDiscovered struct {
	URL              string                        `json:"url"`
	Source           string                        `json:"source"`
	DiscoveredAt     time.Time                     `json:"discovered_at"`
	Identity         identitypkg.DiscoveryIdentity `json:"identity"`
	ExtractionMethod string                        `json:"extraction_method"`
	RawBodySnippet   string                        `json:"raw_body_snippet,omitempty"`
	ContentHash      string                        `json:"content_hash"`
	Metadata         map[string]string             `json:"metadata,omitempty"`
	Headline         string                        `json:"headline,omitempty"`
	Company          string                        `json:"company,omitempty"`
	PublishedAt      string                        `json:"published_at,omitempty"`
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
			PartitionKey: pr.ID,
			Source:       cfg.sourceName(),
			Data:         mustJSON(eventData(ctx, cfg, pr, cfg.Now())),
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

func eventData(ctx context.Context, cfg RunnerConfig, pr PRDiscovery, now time.Time) PressReleaseDiscovered {
	e := PressReleaseDiscovered{URL: pr.URL, Source: cfg.sourceName(), DiscoveredAt: now.UTC(), ExtractionMethod: "regex", Headline: pr.Headline, Company: pr.Company}
	if !pr.Timestamp.IsZero() {
		e.PublishedAt = pr.Timestamp.UTC().Format(time.RFC3339Nano)
	}
	if refs, snippet := discoverTickers(ctx, cfg.Client, cfg.Logger, pr.URL); len(refs) > 0 {
		mintSkuldmarkIDs(refs, cfg.WatchlistTickers, cfg.Logger)
		e.Identity.AllTickers = refs
		first := refs[0]
		e.Identity.PrimaryTicker = &first
		e.RawBodySnippet = snippet
	}
	e.Metadata = map[string]string{"id": pr.ID}
	b, _ := json.Marshal(e)
	sum := sha256.Sum256(b)
	e.ContentHash = hex.EncodeToString(sum[:])
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
			if rec.Event.PartitionKey != "" {
				seen[rec.Event.PartitionKey] = struct{}{}
				continue
			}
			var e PressReleaseDiscovered
			if err := json.Unmarshal(rec.Event.Data, &e); err == nil && e.URL != "" {
				seen[e.URL] = struct{}{}
			}
		}
		from = recs[len(recs)-1].Sequence + 1
	}
}

// mintSkuldmarkIDs fills in SkuldmarkID on any ref whose ticker is on the
// watchlist -- mutates refs in place. Refs for tickers not on the watchlist
// are left with an empty SkuldmarkID, not a guess: prwatch discovers the
// whole PR Newswire firehose, most of which isn't a CIK we have on file.
func mintSkuldmarkIDs(refs []identitypkg.SecurityRef, watchlist map[string]WatchlistRef, logger Logger) {
	if len(watchlist) == 0 {
		return
	}
	for i := range refs {
		wr, ok := watchlist[strings.ToUpper(refs[i].Symbol)]
		if !ok {
			continue
		}
		refs[i].CIK = wr.CIK
		id, err := skuldmarkid.FromSecurityRef(refs[i], wr.Exchange)
		if err != nil {
			if logger != nil {
				logger.Printf("prwatch: skuldmark mint skipped ticker=%s cik=%s: %v", refs[i].Symbol, wr.CIK, err)
			}
			continue
		}
		refs[i].SkuldmarkID = id
	}
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// discoverTickerRetryDelay is how long to wait before a single retry when the first fetch
// succeeds (200 OK) but yields zero ticker refs -- the leading hypothesis (S160-01) is a timing
// race: discovery fires near publish time, while the page's ticker text isn't reliably live at
// the CDN edge yet (prwatch-body's own later-polling fetch of the exact same URL does find the
// ticker text). One bounded retry tests that hypothesis directly without redesigning the event
// model to depend on prwatch-body's separately-scheduled fetch.
var discoverTickerRetryDelay = 5 * time.Second

func discoverTickers(ctx context.Context, c *Client, logger Logger, u string) ([]identitypkg.SecurityRef, string) {
	refs, snippet, ok := fetchAndExtractTickers(ctx, c, logger, u)
	if ok && len(refs) == 0 {
		if logger != nil {
			logger.Printf("prwatch: discoverTickers: no ticker refs on first fetch of %s, retrying once after %s", u, discoverTickerRetryDelay)
		}
		select {
		case <-time.After(discoverTickerRetryDelay):
		case <-ctx.Done():
			return refs, snippet
		}
		if retryRefs, retrySnippet, retryOK := fetchAndExtractTickers(ctx, c, logger, u); retryOK && len(retryRefs) > 0 {
			return retryRefs, retrySnippet
		}
	}
	return refs, snippet
}

// fetchAndExtractTickers does one fetch+extract attempt. The bool return is whether the fetch
// itself succeeded (as opposed to a request/network/read failure) -- distinct from whether any
// ticker refs were found, so discoverTickers can tell "the page loaded but had no tickers yet"
// (worth retrying) apart from "the fetch itself is broken" (retrying won't help).
func fetchAndExtractTickers(ctx context.Context, c *Client, logger Logger, u string) ([]identitypkg.SecurityRef, string, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		if logger != nil {
			logger.Printf("prwatch: discoverTickers: request creation failed for %s: %v", u, err)
		}
		return nil, "", false
	}
	req.Header.Set("User-Agent", c.ua)
	resp, err := c.hc.Do(req)
	if err != nil {
		if logger != nil {
			logger.Printf("prwatch: discoverTickers: fetch failed for %s: %v", u, err)
		}
		return nil, "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if logger != nil {
			logger.Printf("prwatch: discoverTickers: non-200 status %d for %s", resp.StatusCode, u)
		}
		return nil, "", false
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 512<<10))
	if err != nil {
		if logger != nil {
			logger.Printf("prwatch: discoverTickers: body read failed for %s: %v", u, err)
		}
		return nil, "", false
	}
	refs := prid.ExtractFromHTML(body)
	snippet := string(body)
	if len(snippet) > 256 {
		snippet = snippet[:256]
	}
	return refs, snippet, true
}
