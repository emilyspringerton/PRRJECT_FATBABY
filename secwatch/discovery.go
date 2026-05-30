package secwatch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/example/prrject-fatbaby/internal/identity"
	"github.com/example/prrject-fatbaby/internal/issuerregistry"
)

type RunnerConfig struct {
	IssuerRegistry  *issuerregistry.IssuerRegistry
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
				PartitionKey: filing.Identity(),
				Source:       "secwatch",
				Data:         mustJSON(discoveryEventData(filing, cfg.Now(), cfg.IssuerRegistry)),
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

type FilingDiscovered struct {
	Ticker          string                     `json:"ticker,omitempty"`
	CIK             string                     `json:"cik"`
	AccessionNumber string                     `json:"accession_number"`
	FormType        string                     `json:"form_type"`
	DiscoveredAt    time.Time                  `json:"discovered_at"`
	Identity        identity.DiscoveryIdentity `json:"identity"`
	ContentHash     string                     `json:"content_hash"`
	Metadata        map[string]string          `json:"metadata,omitempty"`
	FilingDate      string                     `json:"filing_date,omitempty"`
	PrimaryDocument string                     `json:"primary_document,omitempty"`
	SubmissionsURL  string                     `json:"submissions_source_url,omitempty"`
}

type FilingDiscoveredEvent struct {
	Ticker             string `json:"ticker"`
	CIK                string `json:"cik"`
	AccessionNumber    string `json:"accession_number"`
	Form               string `json:"form"`
	FormType           string `json:"form_type"` // newer FilingDiscovered events use form_type
	FilingDate         string `json:"filing_date"`
	AcceptanceDateTime string `json:"acceptance_datetime,omitempty"`
	PrimaryDocument    string `json:"primary_document"`
	SubmissionsURL     string `json:"submissions_source_url"`
	DiscoveredAt       string `json:"discovered_at"`
}

// EffectiveForm returns the form identifier regardless of which JSON field carried it.
// Newer FilingDiscovered events use "form_type"; older FilingDiscoveredEvent records
// use "form". Callers should use this method instead of reading .Form directly.
func (e *FilingDiscoveredEvent) EffectiveForm() string {
	if e.Form != "" {
		return e.Form
	}
	return e.FormType
}

func discoveryEventData(f Filing, now time.Time, reg *issuerregistry.IssuerRegistry) FilingDiscovered {
	refs := reg.ResolveByCIK(f.CIK)
	if len(refs) == 0 && f.Ticker != "" {
		refs = []identity.SecurityRef{{Exchange: "", Symbol: f.Ticker, CIK: f.CIK, Source: "historical_mapping", Confidence: 0.6}}
	}
	id := identity.DiscoveryIdentity{AllTickers: refs}
	if len(refs) > 0 {
		first := refs[0]
		id.PrimaryTicker = &first
	}
	payload := FilingDiscovered{
		Ticker:          f.Ticker,
		CIK:             f.CIK,
		AccessionNumber: f.AccessionNumber,
		FormType:        f.Form,
		DiscoveredAt:    now.UTC(),
		Identity:        id,
		Metadata:        map[string]string{"acceptance_datetime": f.AcceptanceDateTime},
		FilingDate:      f.FilingDate,
		PrimaryDocument: f.PrimaryDocument,
		SubmissionsURL:  f.SubmissionsURL,
	}
	payload.ContentHash = hashFiling(payload)
	return payload
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
			var e FilingDiscovered
			if err := json.Unmarshal(rec.Event.Data, &e); err == nil && e.CIK != "" && e.AccessionNumber != "" {
				seen[FilingIdentity(e.CIK, e.AccessionNumber)] = struct{}{}
				continue
			}
			var legacy FilingDiscoveredEvent
			if err := json.Unmarshal(rec.Event.Data, &legacy); err == nil && legacy.CIK != "" && legacy.AccessionNumber != "" {
				seen[FilingIdentity(legacy.CIK, legacy.AccessionNumber)] = struct{}{}
			}
		}
		from = recs[len(recs)-1].Sequence + 1
	}
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func hashFiling(f FilingDiscovered) string {
	b, _ := json.Marshal(f)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
