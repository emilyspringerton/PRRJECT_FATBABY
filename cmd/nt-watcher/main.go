// cmd/nt-watcher polls SEC EDGAR for NT 10-K and NT 10-Q (late filing notification)
// filings for every CIK on the watchlist. Late filings are an early-warning signal:
// they precede restatements, material weakness disclosures, and auditor disputes
// at elevated rates compared to on-time filers.
//
// For each NT filing found:
//  1. The primary document is fetched and the stated reason is classified.
//  2. A late_filing signal is emitted to var/entity-graph/signals.ndjson.
//  3. The filing metadata is appended to var/nt-filings/filings.ndjson.
//
// Reason classification:
//   restatement       — "restate", "material error", "accounting error"
//   material_weakness — "material weakness", "internal control", "ICFR"
//   auditor_dispute   — "auditor", "accounting firm", "disagreement"
//   system_transition — "ERP", "system", "implementation"
//   additional_review — "additional time", "complex", "review"
//   unknown           — none of the above matched
//
// Flags:
//   -watchlist   config/watchlist.json
//   -graph-dir   var/entity-graph
//   -out-dir     var/nt-filings
//   -lookback    180            (days; NT filings are rare, use wider window)
//   -poll-interval 12h
//   -one-shot
//   -dry-run
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/example/prrject-fatbaby/internal/entitygraph"
	"github.com/example/prrject-fatbaby/secwatch"
)

const (
	eftsURL   = "https://efts.sec.gov/LATEST/search-index"
	userAgent = "prrject-fatbaby/nt-watcher contact@example.com"
)

// LateReason classifies the stated reason for a late filing.
type LateReason string

const (
	ReasonRestatement      LateReason = "restatement"
	ReasonMaterialWeakness LateReason = "material_weakness"
	ReasonAuditorDispute   LateReason = "auditor_dispute"
	ReasonSystemTransition LateReason = "system_transition"
	ReasonAdditionalReview LateReason = "additional_review"
	ReasonUnknown          LateReason = "unknown"
)

// NTFiling records a discovered NT 10-K or NT 10-Q filing.
type NTFiling struct {
	Ticker      string     `json:"ticker"`
	CIK         string     `json:"cik"`
	FormType    string     `json:"form_type"` // "NT 10-K" or "NT 10-Q"
	FilingDate  string     `json:"filing_date"`
	Accession   string     `json:"accession"`
	Reason      LateReason `json:"reason"`
	ReasonSnippet string   `json:"reason_snippet,omitempty"`
}

type watchlistEntry struct {
	Ticker  string `json:"ticker"`
	CIK     string `json:"cik"`
	Enabled bool   `json:"enabled"`
}
type watchlist struct {
	Entries []watchlistEntry `json:"entries"`
}

type eftsHit struct {
	Source struct {
		FileDate    string `json:"file_date"`
		FormType    string `json:"form_type"`
		AccessionNo string `json:"accession_no"`
	} `json:"_source"`
}
type eftsResponse struct {
	Hits struct {
		Hits []eftsHit `json:"hits"`
	} `json:"hits"`
}

func main() {
	watchlistPath := flag.String("watchlist", filepath.Join("config", "watchlist.json"), "watchlist JSON path")
	graphDir := flag.String("graph-dir", filepath.Join("var", "entity-graph"), "entity-graph data directory")
	outDir := flag.String("out-dir", filepath.Join("var", "nt-filings"), "output directory for filings.ndjson")
	lookback := flag.Int("lookback", 180, "days of EDGAR history per ticker")
	pollInterval := flag.Duration("poll-interval", 12*time.Hour, "interval between EDGAR polls")
	oneShot := flag.Bool("one-shot", false, "exit after one cycle")
	dryRun := flag.Bool("dry-run", false, "log findings without writing")
	flag.Parse()

	logger := log.New(os.Stdout, "nt-watcher ", log.LstdFlags|log.LUTC)

	if !*dryRun {
		for _, d := range []string{*outDir, *graphDir} {
			if err := os.MkdirAll(d, 0o755); err != nil {
				logger.Fatalf("mkdir %s: %v", d, err)
			}
		}
	}

	client := &http.Client{Timeout: 20 * time.Second}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	cfg := pollConfig{graphDir: *graphDir, outDir: *outDir, lookbackDays: *lookback, dryRun: *dryRun}

	for {
		if err := runPoll(ctx, logger, client, *watchlistPath, cfg); err != nil {
			logger.Printf("poll error: %v", err)
		}
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

type pollConfig struct {
	graphDir     string
	outDir       string
	lookbackDays int
	dryRun       bool
}

func runPoll(ctx context.Context, logger *log.Logger, client *http.Client, watchlistPath string, cfg pollConfig) error {
	wl, err := loadWatchlist(watchlistPath)
	if err != nil {
		return fmt.Errorf("load watchlist: %w", err)
	}

	seen := loadSeen(cfg.outDir)
	since := time.Now().UTC().AddDate(0, 0, -cfg.lookbackDays).Format("2006-01-02")

	var newFilings []NTFiling
	var signals []entitygraph.Signal

	for _, entry := range wl.Entries {
		if !entry.Enabled {
			continue
		}
		filings, err := fetchNTFilings(ctx, client, entry, since)
		if err != nil {
			logger.Printf("fetch ticker=%s err=%v (skipping)", entry.Ticker, err)
			continue
		}
		for _, f := range filings {
			if seen[f.Accession] {
				continue
			}
			// Fetch primary document to classify reason.
			reason, snippet := classifyFromEFTS(ctx, client, f.CIK, f.Accession)
			f.Reason = reason
			f.ReasonSnippet = snippet
			newFilings = append(newFilings, f)
			signals = append(signals, scoreNTFiling(f))
			logger.Printf("NT filing ticker=%s form=%s date=%s reason=%s", f.Ticker, f.FormType, f.FilingDate, f.Reason)
		}
	}

	if len(newFilings) == 0 {
		logger.Printf("poll complete: no new NT filings (lookback=%dd)", cfg.lookbackDays)
		return nil
	}

	if cfg.dryRun {
		return nil
	}

	if err := appendFilings(cfg.outDir, newFilings); err != nil {
		logger.Printf("write filings: %v", err)
	}
	if len(signals) > 0 {
		if err := entitygraph.WriteSignals(cfg.graphDir, signals); err != nil {
			logger.Printf("write signals: %v", err)
		}
	}
	return nil
}

func fetchNTFilings(ctx context.Context, client *http.Client, entry watchlistEntry, since string) ([]NTFiling, error) {
	params := url.Values{}
	params.Set("q", fmt.Sprintf(`"%s"`, padCIK(entry.CIK)))
	params.Set("forms", "NT 10-K,NT 10-Q,NT 10-K/A,NT 10-Q/A")
	params.Set("dateRange", "custom")
	params.Set("startdt", since)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, eftsURL+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return nil, fmt.Errorf("EDGAR EFTS status %d: %s", resp.StatusCode, string(b))
	}
	var result eftsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	var out []NTFiling
	for _, h := range result.Hits.Hits {
		if h.Source.FormType == "" || h.Source.FileDate == "" || h.Source.AccessionNo == "" {
			continue
		}
		out = append(out, NTFiling{
			Ticker:     entry.Ticker,
			CIK:        secwatch.NormalizeCIK(entry.CIK),
			FormType:   h.Source.FormType,
			FilingDate: h.Source.FileDate,
			Accession:  h.Source.AccessionNo,
		})
	}
	return out, nil
}

// classifyFromEFTS fetches the NT filing document index and reads the first ~2000
// bytes of the primary document to classify the stated reason.
func classifyFromEFTS(ctx context.Context, client *http.Client, cik, accession string) (LateReason, string) {
	normCIK := secwatch.NormalizeCIK(cik)
	cleanAcc := strings.ReplaceAll(accession, "-", "")
	cikStripped := strings.TrimLeft(normCIK, "0")
	docURL := fmt.Sprintf("https://www.sec.gov/Archives/edgar/data/%s/%s/%s-index.htm",
		cikStripped, cleanAcc, accession)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, docURL, nil)
	if err != nil {
		return ReasonUnknown, ""
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return ReasonUnknown, ""
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))

	// Rate limit.
	time.Sleep(250 * time.Millisecond)
	return classifyReason(string(body))
}

func classifyReason(text string) (LateReason, string) {
	lower := strings.ToLower(text)
	snippet := extractSnippet(text, 200)

	switch {
	case anyContains(lower, "restat", "material error", "accounting error", "accounting restat"):
		return ReasonRestatement, snippet
	case anyContains(lower, "material weakness", "internal control", "icfr", "internal controls over financial reporting"):
		return ReasonMaterialWeakness, snippet
	case anyContains(lower, "auditor", "accounting firm", "public accounting", "disagreement with"):
		return ReasonAuditorDispute, snippet
	case anyContains(lower, "erp", "system transition", "accounting system", "software implementation", "system implementation"):
		return ReasonSystemTransition, snippet
	case anyContains(lower, "additional time", "complex", "additional review", "review process", "compilation"):
		return ReasonAdditionalReview, snippet
	default:
		return ReasonUnknown, snippet
	}
}

func anyContains(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func extractSnippet(text string, maxLen int) string {
	t := strings.TrimSpace(text)
	if len(t) > maxLen {
		return t[:maxLen] + "…"
	}
	return t
}

func scoreNTFiling(f NTFiling) entitygraph.Signal {
	today := time.Now().UTC().Format("2006-01-02")
	validThrough := time.Now().UTC().AddDate(0, 9, 0).Format("2006-01-02")

	sev := entitygraph.SeverityMedium
	conf := 0.72
	score := 0.50

	switch f.Reason {
	case ReasonRestatement, ReasonMaterialWeakness:
		sev = entitygraph.SeverityHigh
		conf = 0.82
		score = 0.80
	case ReasonAuditorDispute:
		sev = entitygraph.SeverityHigh
		conf = 0.78
		score = 0.75
	}

	what := "annual" // NT 10-K
	if strings.Contains(f.FormType, "10-Q") {
		what = "quarterly"
	}

	interp := fmt.Sprintf("%s filed a late %s report (%s) on %s — reason: %s. Late filings precede restatements, material weakness disclosures, and auditor disputes at elevated base rates. Monitor next filing closely.",
		f.Ticker, what, f.FormType, f.FilingDate, f.Reason)

	return entitygraph.Signal{
		SignalID:       fmt.Sprintf("late_filing_%s_%s", strings.ToLower(f.Ticker), strings.ReplaceAll(f.FilingDate, "-", "")),
		Type:           entitygraph.SignalLateFiling,
		Ticker:         f.Ticker,
		Severity:       sev,
		Confidence:     conf,
		Score:          score,
		DetectedAt:     today,
		FilingDate:     f.FilingDate,
		ValidThrough:   validThrough,
		Interpretation: interp,
		Metadata: map[string]string{
			"form_type":  f.FormType,
			"reason":     string(f.Reason),
			"accession":  f.Accession,
		},
	}
}

// ── Persistence ───────────────────────────────────────────────────────────────

func loadSeen(outDir string) map[string]bool {
	seen := map[string]bool{}
	f, err := os.Open(filepath.Join(outDir, "filings.ndjson"))
	if err != nil {
		return seen
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	for {
		var nf NTFiling
		if err := dec.Decode(&nf); err != nil {
			break
		}
		seen[nf.Accession] = true
	}
	return seen
}

func appendFilings(outDir string, filings []NTFiling) error {
	f, err := os.OpenFile(filepath.Join(outDir, "filings.ndjson"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for i := range filings {
		if err := enc.Encode(&filings[i]); err != nil {
			return err
		}
	}
	return nil
}

func loadWatchlist(path string) (watchlist, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return watchlist{}, err
	}
	var wl watchlist
	return wl, json.Unmarshal(b, &wl)
}

func padCIK(cik string) string {
	d := ""
	for _, r := range cik {
		if r >= '0' && r <= '9' {
			d += string(r)
		}
	}
	for len(d) < 10 {
		d = "0" + d
	}
	return d
}
