// guidance-watcher polls the prwatch-body event store for pr_body_fetched events,
// runs guidance extraction on each press release, and for publishable results
// appends a guidance.Article to var/guidance/articles.ndjson.
//
// It mirrors the eps-processor pipeline exactly: same event sources, same cursor
// pattern, same ticker map from pr_discovered events.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/example/prrject-fatbaby/internal/guidance"
	"github.com/example/prrject-fatbaby/prwatch"
)

func main() {
	discoveryRoot := flag.String("discovery-store", filepath.Join("var", "prwatch"), "prwatch discovery event store root")
	bodyRoot      := flag.String("body-store", filepath.Join("var", "prwatch-body"), "prwatch body event store root")
	outDir        := flag.String("out-dir", filepath.Join("var", "guidance"), "output directory for articles.ndjson")
	pollInterval  := flag.Duration("poll-interval", 30*time.Second, "poll interval")
	cursorPath    := flag.String("cursor", filepath.Join("var", "guidance-watcher", ".cursor"), "cursor file")
	batchSize     := flag.Int("batch-size", 256, "events per poll")
	oneShot       := flag.Bool("one-shot", false, "process one batch and exit")
	flag.Parse()

	logger := log.New(os.Stdout, "guidance-watcher ", log.LstdFlags|log.LUTC)

	for _, dir := range []string{*outDir, filepath.Dir(*cursorPath)} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			logger.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	discoveryStore, err := eventstore.NewFileStore(*discoveryRoot)
	if err != nil {
		logger.Fatalf("open discovery store: %v", err)
	}
	defer discoveryStore.Close()

	bodyStore, err := eventstore.NewFileStore(*bodyRoot)
	if err != nil {
		logger.Fatalf("open body store: %v", err)
	}
	defer bodyStore.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	logger.Printf("starting poll_interval=%s body_store=%s out_dir=%s", *pollInterval, *bodyRoot, *outDir)

	cursor := loadCursor(*cursorPath, logger)

	for {
		tickerByID := buildTickerMap(ctx, discoveryStore, logger)

		cursor = runBatch(ctx, bodyStore, tickerByID, logger, batchConfig{
			outDir:     *outDir,
			cursorPath: *cursorPath,
			batchSize:  *batchSize,
			cursor:     cursor,
		})

		if *oneShot {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(*pollInterval):
		}
	}
}

type batchConfig struct {
	outDir     string
	cursorPath string
	batchSize  int
	cursor     uint64
}

func runBatch(ctx context.Context, bodyStore eventstore.EventStore, tickerByID map[string]string, logger *log.Logger, cfg batchConfig) uint64 {
	recs, err := bodyStore.ReadFrom(ctx, cfg.cursor, cfg.batchSize)
	if err != nil {
		logger.Printf("read batch cursor=%d err=%v", cfg.cursor, err)
		return cfg.cursor
	}
	if len(recs) == 0 {
		return cfg.cursor
	}

	published := 0
	skipped := 0

	for _, rec := range recs {
		if rec.Event.Type != "pr_body_fetched" {
			continue
		}
		var ev prwatch.BodyFetchedEvent
		if err := json.Unmarshal(rec.Event.Data, &ev); err != nil {
			continue
		}
		ticker := tickerByID[ev.PRDiscoveryID]
		if ticker == "" {
			skipped++
			continue
		}
		if len(ev.Body) < 200 {
			skipped++
			continue
		}

		if isLitigationAlertHeadline(ev.Headline) {
			skipped++
			continue
		}

		sourceIdentity := "pr:" + ev.PRDiscoveryID
		g := guidance.Extract(ev.Body, sourceIdentity, ticker)
		if !g.HasGuidance {
			skipped++
			continue
		}
		if g.Issuer == "" && ev.Headline != "" {
			g.Issuer = extractIssuerFromTitle(ev.Headline)
		}

		art, ok := guidance.Generate(g, time.Now().UTC())
		if !ok {
			skipped++
			continue
		}

		if err := appendArticle(cfg.outDir, art); err != nil {
			logger.Printf("append article err=%v", err)
			continue
		}
		published++
		logger.Printf("guidance published ticker=%s action=%s metric=%s confidence=%.2f id=%s",
			art.Ticker, art.Action, art.Metric, art.Confidence, art.ID)
	}

	logger.Printf("batch done published=%d skipped=%d", published, skipped)

	newCursor := recs[len(recs)-1].Sequence + 1
	writeCursor(cfg.cursorPath, newCursor, logger)
	return newCursor
}

func appendArticle(dir string, art guidance.Article) error {
	path := filepath.Join(dir, "articles.ndjson")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	line, err := json.Marshal(art)
	if err != nil {
		return err
	}
	_, err = f.Write(append(line, '\n'))
	return err
}

func loadCursor(path string, logger *log.Logger) uint64 {
	b, err := os.ReadFile(path)
	if err != nil {
		return 1
	}
	var v uint64
	if err := json.Unmarshal(b, &v); err != nil {
		return 1
	}
	logger.Printf("cursor loaded seq=%d from %s", v, path)
	return v
}

func writeCursor(path string, seq uint64, logger *log.Logger) {
	b, err := json.Marshal(seq)
	if err != nil {
		return
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		logger.Printf("write cursor: %v", err)
	}
}

func buildTickerMap(ctx context.Context, store eventstore.EventStore, logger *log.Logger) map[string]string {
	m := make(map[string]string)
	err := store.Scan(ctx, 1, func(rec eventstore.Record) error {
		if rec.Event.Type != "pr_discovered" {
			return nil
		}
		var ev prwatch.PressReleaseDiscovered
		if err := json.Unmarshal(rec.Event.Data, &ev); err != nil {
			return nil
		}
		if ev.Identity.PrimaryTicker != nil && ev.Identity.PrimaryTicker.Symbol != "" {
			if id, ok := ev.Metadata["id"]; ok {
				m[id] = ev.Identity.PrimaryTicker.Symbol
			}
		}
		return nil
	})
	if err != nil && logger != nil {
		logger.Printf("ticker map scan error: %v", err)
	}
	return m
}

// reHeadlineTimePrefix strips a leading "HH:MM ET" scrape artifact -- and the
// blank/tab-indented lines PRNewswire's own listing markup wraps around it --
// that prwatch's discovery scraper (prwatch/client.go's h3Re) captures
// alongside the real title text. Confirmed live, S170-07: essentially every
// ev.Headline in var/guidance carries this, e.g. "13:46 ET\n\t\t\t\n\t\t\t\n
// \t\t\tPNR SHAREHOLDER INVESTIGATION: ...". Left unfixed, it either garbles
// the "Reports/Announces/..." keyword split below (the keyword still matches,
// but the returned issuer carries the timestamp prefix) or, worse, dominates
// the 40-char fallback so the "issuer" is mostly timestamp noise plus a few
// truncated real characters. Fixing this at the scrape source (prwatch/
// client.go) would touch every other watcher reading ev.Headline -- out of
// this ticket's scope, so it's stripped defensively here instead.
var reHeadlineTimePrefix = regexp.MustCompile(`(?s)^\s*\d{1,2}:\d{2}\s*ET\s*`)

// reLitigationAlertHeadline matches the securities-litigation-solicitation
// genre of PRNewswire press release: a law firm (SueWallSt, Pomerantz, Levi &
// Korsinsky, Rosen, Robbins Geller, Wolf Haldenstein, Hagens Berman, The
// Gross Law Firm, Kahn Swick, Bragar Eagel, and others not worth enumerating
// individually) soliciting plaintiffs against a company whose ticker just
// happens to appear in the release, e.g. "PNR SHAREHOLDER INVESTIGATION:
// SueWallSt Notifies Investors of Potential Securities Claims Involving
// Pentair plc". These aren't guidance from the company at all -- confirmed
// live, S170-07: pulling var/guidance/articles.ndjson found this genre was
// not a rare edge case but the overwhelming majority of the live dataset,
// each one a fabricated "guidance" article (a real EPS figure quoted
// somewhere *inside* the litigation-alert copy, attributed to a company that
// never issued it) attached to a garbled issuer name via the fallback path
// above. Matched on phrase, not law-firm name, since the roster of firms
// running this playbook is long and any name list would immediately go
// stale; these phrases are near-universal boilerplate across the entire
// genre regardless of which firm wrote it (also covers the closely-related
// data-breach-class-action genre, e.g. Edelson Lechtzin's "Data Breach"
// alerts, and generic M&A-fairness solicitations like "Investigating
// Whether X Are Obtaining Fair Deals for their Shareholders" -- same
// solicit-plaintiffs playbook, different pretext). Measured against the
// full var/prwatch headline corpus (9625 unique headlines) before shipping:
// flags ~12%, with a manual review of both the flagged and unflagged sides
// finding no genuine company press release caught and no more than a stray,
// low-volume long tail of spam phrasing left uncaught -- good enough to
// stop fabricating guidance data, not a claim of exhaustive coverage.
var reLitigationAlertHeadline = regexp.MustCompile(`(?i)\b(shareholder alert|shareholder investigation|investor alert|` +
	`investor investigation|class action|securities claims|securities fraud|securities lawsuit|` +
	`securities law violations|notifies investors|reminds (?:shareholders|investors)|deadline alert|` +
	`lead plaintiff deadline|encourages investors|opportunity to lead|fair deals for|fiduciary duties|` +
	`data breach|law firm|lost money|investigat\w*)\b`)

// isLitigationAlertHeadline reports whether title reads as a securities-
// litigation solicitation rather than a real company press release -- see
// reLitigationAlertHeadline's own doc comment. Applied before extraction
// (not after): unlike extractIssuerFromTitle's fallback, which just produces
// an ugly issuer name, letting one of these through guidance.Extract can
// fabricate an entire "company raises guidance" article from numbers quoted
// out of context inside the litigation copy.
func isLitigationAlertHeadline(title string) bool {
	return reLitigationAlertHeadline.MatchString(title)
}

// extractIssuerFromTitle pulls the company name from a press release title.
// Press release titles often start with "Company Name Reports ..." or
// "Company Name Announces ...".
func extractIssuerFromTitle(title string) string {
	title = strings.TrimSpace(reHeadlineTimePrefix.ReplaceAllString(title, ""))
	for _, kw := range []string{" Reports ", " Announces ", " Provides ", " Issues ", " Updates "} {
		if idx := strings.Index(title, kw); idx > 0 {
			return strings.TrimSpace(title[:idx])
		}
	}
	// Fallback: take the first 40 chars.
	runes := []rune(title)
	if len(runes) > 40 {
		runes = runes[:40]
	}
	return strings.TrimSpace(string(runes))
}
