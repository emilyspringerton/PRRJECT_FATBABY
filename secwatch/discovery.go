package secwatch

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
)

type RunnerConfig struct {
	WatchlistPath   string
	StoreRoot       string
	DryRun          bool
	Concurrency     int
	PollIntervalJit time.Duration
	Now             func() time.Time
	Logger          Logger
	Client          *Client
}

type Summary struct {
	Watched       int
	CompaniesOK   int
	CompaniesFail int
	SeenSkipped   int
	Discovered    int
}

type Logger interface {
	Printf(format string, args ...any)
}

type stdLogger struct{}

func (stdLogger) Printf(format string, args ...any) {
	fmt.Printf(format+"\n", args...)
}

func RunDiscovery(ctx context.Context, cfg RunnerConfig) (Summary, error) {
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	if cfg.Logger == nil {
		cfg.Logger = stdLogger{}
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 2
	}
	if cfg.Client == nil {
		cfg.Client = NewClient(ClientConfig{})
	}

	watchlist, err := LoadWatchlist(cfg.WatchlistPath)
	if err != nil {
		return Summary{}, err
	}
	entries := watchlist.EnabledEntries()
	sort.Slice(entries, func(i, j int) bool { return entries[i].Ticker < entries[j].Ticker })

	store, err := eventstore.NewFileStore(cfg.StoreRoot)
	if err != nil {
		return Summary{}, fmt.Errorf("open event store: %w", err)
	}
	defer store.Close()

	seen, err := LoadSeenIdentities(ctx, store)
	if err != nil {
		return Summary{}, err
	}

	type result struct {
		entry   WatchEntry
		filings []Filing
		err     error
	}
	jobs := make(chan WatchEntry)
	results := make(chan result)
	for i := 0; i < cfg.Concurrency; i++ {
		go func() {
			for entry := range jobs {
				r := result{entry: entry}
				body, err := cfg.Client.FetchSubmissions(ctx, entry.CIK)
				if err != nil {
					r.err = fmt.Errorf("ticker=%s cik=%s: %w", entry.Ticker, entry.CIK, err)
					results <- r
					continue
				}
				filings, err := ParseRecentFilings(body, entry.Ticker)
				if err != nil {
					r.err = fmt.Errorf("ticker=%s parse submissions: %w", entry.Ticker, err)
					results <- r
					continue
				}
				r.filings = FilterByAllowedForms(filings, entry.AllowedForms)
				results <- r
			}
		}()
	}

	go func() {
		defer close(jobs)
		for _, entry := range entries {
			select {
			case <-ctx.Done():
				return
			case jobs <- entry:
			}
		}
	}()

	summary := Summary{Watched: len(entries)}
	for i := 0; i < len(entries); i++ {
		r := <-results
		if r.err != nil {
			summary.CompaniesFail++
			cfg.Logger.Printf("secwatch company failed ticker=%s err=%v", r.entry.Ticker, r.err)
			continue
		}
		summary.CompaniesOK++
		sort.Slice(r.filings, func(i, j int) bool { return r.filings[i].Identity() < r.filings[j].Identity() })
		for _, filing := range r.filings {
			if _, ok := seen[filing.Identity()]; ok {
				summary.SeenSkipped++
				continue
			}
			summary.Discovered++
			if cfg.DryRun {
				cfg.Logger.Printf("secwatch dry-run new filing ticker=%s cik=%s accession=%s form=%s filing_date=%s", filing.Ticker, filing.CIK, filing.AccessionNumber, filing.Form, filing.FilingDate)
				continue
			}
			ev := eventstore.Event{
				ID:           "filing_discovered:" + filing.Identity(),
				Type:         "filing_discovered",
				OccurredAt:   cfg.Now(),
				AggregateKey: filing.Identity(),
				Source:       "secwatch",
				Data:         mustJSON(discoveryEventData(filing, cfg.Now())),
			}
			if _, err := store.Append(ctx, ev); err != nil {
				return summary, fmt.Errorf("persist discovered filing %s: %w", filing.Identity(), err)
			}
			seen[filing.Identity()] = struct{}{}
			cfg.Logger.Printf("secwatch discovered ticker=%s cik=%s accession=%s form=%s", filing.Ticker, filing.CIK, filing.AccessionNumber, filing.Form)
		}
	}
	cfg.Logger.Printf("secwatch summary watched=%d ok=%d failed=%d discovered=%d already_seen=%d dry_run=%t", summary.Watched, summary.CompaniesOK, summary.CompaniesFail, summary.Discovered, summary.SeenSkipped, cfg.DryRun)
	return summary, nil
}

type FilingDiscoveredEvent struct {
	Ticker             string `json:"ticker"`
	CIK                string `json:"cik"`
	AccessionNumber    string `json:"accession_number"`
	Form               string `json:"form"`
	FilingDate         string `json:"filing_date"`
	AcceptanceDateTime string `json:"acceptance_datetime,omitempty"`
	PrimaryDocument    string `json:"primary_document"`
	SubmissionsURL     string `json:"submissions_source_url"`
	DiscoveredAt       string `json:"discovered_at"`
}

func discoveryEventData(f Filing, now time.Time) FilingDiscoveredEvent {
	return FilingDiscoveredEvent{
		Ticker:             f.Ticker,
		CIK:                f.CIK,
		AccessionNumber:    f.AccessionNumber,
		Form:               f.Form,
		FilingDate:         f.FilingDate,
		AcceptanceDateTime: f.AcceptanceDateTime,
		PrimaryDocument:    f.PrimaryDocument,
		SubmissionsURL:     f.SubmissionsURL,
		DiscoveredAt:       now.UTC().Format(time.RFC3339Nano),
	}
}

func LoadSeenIdentities(ctx context.Context, store eventstore.EventStore) (map[string]struct{}, error) {
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
			if rec.Event.Type != "filing_discovered" {
				continue
			}
			var e FilingDiscoveredEvent
			if err := json.Unmarshal(rec.Event.Data, &e); err != nil {
				continue
			}
			if e.CIK != "" && e.AccessionNumber != "" {
				seen[FilingIdentity(e.CIK, e.AccessionNumber)] = struct{}{}
			}
		}
		from = recs[len(recs)-1].Sequence + 1
	}
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
