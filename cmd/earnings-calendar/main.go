// earnings-calendar builds and maintains the earnings date calendar.
//
// Three data sources processed on each run:
//
//  1. EPS articles (var/eps/articles.ndjson) — backfill historical dates.
//     PublishAt + Period → "backfilled" status. One-time at startup, then on
//     each poll cycle to pick up newly published EPS articles.
//
//  2. Press releases (var/prwatch-body) — scan for "will report on [date]"
//     announcements. Emits "announced" status records.
//
//  3. 8-K filings (var/secwatch) — when a source_document_persisted event
//     carries an 8-K with Item 2.02, the filing date = actual report date.
//     Emits "confirmed" status records, overwriting any existing announced entry.
//
// Output: var/earnings-calendar/dates.ndjson (append-only, last-record-wins dedup).
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/example/prrject-fatbaby/internal/earningscal"
	"github.com/example/prrject-fatbaby/internal/eps"
	"github.com/example/prrject-fatbaby/pkg/intelligence"
	"github.com/example/prrject-fatbaby/prwatch"
	"github.com/example/prrject-fatbaby/secwatch"
)

func main() {
	epsDir         := flag.String("eps-dir", filepath.Join("var", "eps"), "EPS articles directory (backfill source)")
	bodyRoot       := flag.String("body-store", filepath.Join("var", "prwatch-body"), "prwatch body event store")
	secRoot        := flag.String("sec-store", filepath.Join("var", "secwatch"), "secwatch event store (8-K confirmed dates)")
	outDir         := flag.String("out-dir", filepath.Join("var", "earnings-calendar"), "output directory")
	watchlistPath  := flag.String("watchlist", filepath.Join("config", "watchlist.json"), "watchlist.json for ticker resolver")
	pollInterval   := flag.Duration("poll-interval", 60*time.Second, "poll interval")
	cursorPath     := flag.String("cursor", filepath.Join("var", "earnings-calendar", ".cursor"), "PR body cursor file")
	secCursorPath  := flag.String("sec-cursor", filepath.Join("var", "earnings-calendar", ".sec-cursor"), "secwatch cursor file")
	batchSize      := flag.Int("batch-size", 512, "events per poll")
	oneShot        := flag.Bool("one-shot", false, "process once and exit")
	flag.Parse()

	logger := log.New(os.Stdout, "earnings-calendar ", log.LstdFlags|log.LUTC)

	// Build watchlist-based company→ticker resolver so PR-announced records
	// are only accepted for companies we actually care about.
	var resolver *earningscal.CompanyResolver
	if wl, err := secwatch.LoadWatchlist(*watchlistPath); err != nil {
		logger.Printf("warn: could not load watchlist for resolver: %v", err)
	} else {
		entries := make([][2]string, 0, len(wl.Entries))
		for _, e := range wl.Entries {
			if e.Enabled && e.CompanyName != "" {
				entries = append(entries, [2]string{e.Ticker, e.CompanyName})
			}
		}
		resolver = earningscal.NewCompanyResolver(entries)
		logger.Printf("watchlist resolver: %d companies loaded", len(entries))
	}

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		logger.Fatalf("mkdir %s: %v", *outDir, err)
	}

	store := earningscal.NewStore(*outDir)
	if err := store.Refresh(); err != nil {
		logger.Printf("initial load: %v", err)
	}
	logger.Printf("loaded existing records=%d", store.Count())

	bodyStore, err := eventstore.NewFileStore(*bodyRoot)
	if err != nil {
		logger.Fatalf("open body store: %v", err)
	}
	defer bodyStore.Close()

	secStore, err := eventstore.NewFileStore(*secRoot)
	if err != nil {
		logger.Fatalf("open secwatch store: %v", err)
	}
	defer secStore.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	prCursor  := loadCursorFile(*cursorPath)
	secCursor := loadCursorFile(*secCursorPath)

	for {
		// Source 1: backfill from EPS articles (always rescan; cheap and idempotent).
		backfilled := backfillFromEPS(*epsDir, store, logger)
		if backfilled > 0 {
			logger.Printf("backfill eps_articles=%d new records", backfilled)
		}

		// Source 2: press release body scan for "will report on [date]".
		prNew, prCursorNew := scanPRBodies(ctx, bodyStore, store, resolver, prCursor, *batchSize, logger)
		if prNew > 0 {
			logger.Printf("pr scan new=%d", prNew)
		}
		prCursor = prCursorNew
		saveCursorFile(*cursorPath, prCursor)

		// Source 3: 8-K filings — confirmed dates when Item 2.02 is present.
		secNew, secCursorNew := scanSecFilings(ctx, secStore, store, secCursor, *batchSize, logger)
		if secNew > 0 {
			logger.Printf("sec scan new=%d", secNew)
		}
		secCursor = secCursorNew
		saveCursorFile(*secCursorPath, secCursor)

		if *oneShot {
			logger.Printf("one-shot done total=%d", store.Count())
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(*pollInterval):
		}
	}
}

// ── Source 1: EPS article backfill ──────────────────────────────────────────

func backfillFromEPS(epsDir string, store *earningscal.Store, logger *log.Logger) int {
	path := filepath.Join(epsDir, "articles.ndjson")
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		logger.Printf("open eps articles: %v", err)
		return 0
	}
	defer f.Close()

	n := 0
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 4<<20), 4<<20)
	for sc.Scan() {
		var a eps.Article
		if err := json.Unmarshal(sc.Bytes(), &a); err != nil {
			continue
		}
		if a.Ticker == "" || a.PublishAt == "" || a.Period.FiscalQuarter == "" {
			continue
		}
		publishTime, err := time.Parse(time.RFC3339, a.PublishAt)
		if err != nil {
			continue
		}
		reportDate := publishTime.UTC().Format("2006-01-02")
		id := earningscal.MakeID(a.Ticker, a.Period.FiscalQuarter, a.Period.FiscalYear)

		// Only write if we don't already have a better record.
		existing := store.GetByID(id)
		if existing != nil && earningscal.StatusPriority(existing.Status) >= earningscal.StatusPriority(earningscal.StatusBackfilled) {
			continue
		}

		d := earningscal.EarningsDate{
			ID:            id,
			Ticker:        a.Ticker,
			FiscalQuarter: a.Period.FiscalQuarter,
			FiscalYear:    a.Period.FiscalYear,
			ReportDate:    reportDate,
			Status:        earningscal.StatusBackfilled,
			Source:        "eps_article",
			SourceID:      a.SourceIdentity,
			UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
		}
		if err := store.Append(d); err != nil {
			logger.Printf("append backfill: %v", err)
			continue
		}
		n++
	}
	return n
}

// ── Source 2: PR body scan ───────────────────────────────────────────────────

func scanPRBodies(ctx context.Context, bodyStore eventstore.EventStore, store *earningscal.Store, resolver *earningscal.CompanyResolver, cursor uint64, batchSize int, logger *log.Logger) (int, uint64) {
	recs, err := bodyStore.ReadFrom(ctx, cursor, batchSize)
	if err != nil {
		logger.Printf("read pr bodies: %v", err)
		return 0, cursor
	}
	if len(recs) == 0 {
		return 0, cursor
	}

	n := 0
	for _, rec := range recs {
		if rec.Event.Type != "pr_body_fetched" {
			continue
		}
		var ev prwatch.BodyFetchedEvent
		if err := json.Unmarshal(rec.Event.Data, &ev); err != nil {
			continue
		}
		if len(ev.Body) < 100 {
			continue
		}

		reportDate, ok := earningscal.ExtractDate(ev.Body)
		if !ok {
			continue
		}
		quarter, year := earningscal.ExtractPeriod(ev.Body)
		if quarter == "" && year == 0 {
			continue // can't identify which earnings this is for
		}
		// Resolve ticker via watchlist-based company name matcher.
		// Only emit announced records for companies on our watchlist.
		ticker := ""
		candidate := ev.Company
		if candidate == "" {
			candidate = extractIssuer(ev.Headline)
		}
		if resolver != nil {
			ticker = resolver.Resolve(candidate)
		}
		if ticker == "" {
			continue // not a watchlisted company; skip to avoid false positives
		}

		id := earningscal.MakeID(ticker, quarter, year)
		existing := store.GetByID(id)
		if existing != nil && earningscal.StatusPriority(existing.Status) >= earningscal.StatusPriority(earningscal.StatusAnnounced) {
			continue
		}

		bm := earningscal.ExtractBeforeMarket(ev.Body)
		issuer := earningscal.CleanIssuer(candidate)

		d := earningscal.EarningsDate{
			ID:            id,
			Ticker:        ticker,
			Issuer:        issuer,
			FiscalQuarter: quarter,
			FiscalYear:    year,
			ReportDate:    reportDate,
			BeforeMarket:  bm,
			Status:        earningscal.StatusAnnounced,
			Source:        "press_release",
			SourceID:      ev.PRDiscoveryID,
			UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
		}
		if err := store.Append(d); err != nil {
			logger.Printf("append pr: %v", err)
			continue
		}
		n++
	}

	return n, recs[len(recs)-1].Sequence + 1
}

// ── Source 3: 8-K filing confirmed dates ────────────────────────────────────

func scanSecFilings(ctx context.Context, secStore eventstore.EventStore, store *earningscal.Store, cursor uint64, batchSize int, logger *log.Logger) (int, uint64) {
	recs, err := secStore.ReadFrom(ctx, cursor, batchSize)
	if err != nil {
		logger.Printf("read sec store: %v", err)
		return 0, cursor
	}
	if len(recs) == 0 {
		return 0, cursor
	}

	n := 0
	for _, rec := range recs {
		if rec.Event.Type != "source_document_persisted" {
			continue
		}
		var doc intelligence.SourceDocument
		if err := json.Unmarshal(rec.Event.Data, &doc); err != nil {
			continue
		}
		// Only process 8-K filings.
		is8K := doc.Form == "8-K" || doc.SourceType == "sec_8k" || strings.Contains(strings.ToUpper(doc.DocumentURL), "8-K")
		if !is8K || doc.Ticker == "" || doc.FilingDate == "" {
			continue
		}
		// Must contain Item 2.02 language to be an earnings filing.
		if !strings.Contains(doc.CleanedText, "2.02") && !strings.Contains(doc.CleanedText, "Results of Operations") {
			continue
		}

		quarter, year := earningscal.ExtractPeriod(doc.CleanedText)
		if quarter == "" && year == 0 {
			continue
		}
		if year == 0 {
			// ExtractPeriod found a quarter label ("FY", "Q1", ...) but no
			// explicit year in the filing text — fall back to the filing
			// date's year rather than silently storing FiscalYear=0 (found
			// live: 61 of 363 stored records had this, all "confirmed"
			// 8-K-sourced, since this is the only path that skipped past
			// the quarter=="" && year==0 guard with a nonzero quarter).
			if t, err := time.Parse("2006-01-02", doc.FilingDate); err == nil {
				year = t.Year()
			}
		}

		id := earningscal.MakeID(doc.Ticker, quarter, year)
		existing := store.GetByID(id)
		// Confirmed always overwrites anything lower.
		if existing != nil && earningscal.StatusPriority(existing.Status) >= earningscal.StatusPriority(earningscal.StatusConfirmed) {
			continue
		}

		d := earningscal.EarningsDate{
			ID:            id,
			Ticker:        doc.Ticker,
			FiscalQuarter: quarter,
			FiscalYear:    year,
			ReportDate:    doc.FilingDate,
			Status:        earningscal.StatusConfirmed,
			Source:        "8k_filing",
			SourceID:      doc.Identity,
			UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
		}
		if err := store.Append(d); err != nil {
			logger.Printf("append sec: %v", err)
			continue
		}
		n++
		logger.Printf("confirmed ticker=%s period=%s %d date=%s", doc.Ticker, quarter, year, doc.FilingDate)
	}

	return n, recs[len(recs)-1].Sequence + 1
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func loadCursorFile(path string) uint64 {
	b, err := os.ReadFile(path)
	if err != nil {
		return 1
	}
	var v uint64
	if err := json.Unmarshal(b, &v); err != nil {
		return 1
	}
	return v
}

func saveCursorFile(path string, seq uint64) {
	b, _ := json.Marshal(seq)
	_ = os.WriteFile(path, b, 0o644)
}

// slugTicker creates a rough ticker guess from a company name (lowercase, first word).
// A proper implementation would use the watchlist ticker map.
func slugTicker(company string) string {
	words := strings.Fields(strings.ToUpper(company))
	if len(words) == 0 {
		return ""
	}
	return words[0]
}

func extractIssuer(headline string) string {
	for _, kw := range []string{" to Report", " Will Report", " Announces", " Schedules"} {
		if idx := strings.Index(headline, kw); idx > 0 {
			return strings.TrimSpace(headline[:idx])
		}
	}
	return headline
}
