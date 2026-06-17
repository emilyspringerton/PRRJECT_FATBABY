// cmd/form4-watcher polls SEC EDGAR for Form 4 (insider transaction) filings
// for every CIK on the watchlist, parses conviction open-market transactions
// (codes P and S), emits insider_buy and insider_sell_cluster signals to the
// entity-graph signal store, and writes the raw transactions to
// var/form4/transactions.ndjson.
//
// Signals are written to var/entity-graph/signals.ndjson so they are picked up
// by Emily, Jon's divergence engine, and the governance health index.
//
// Flags:
//
//	-watchlist   config/watchlist.json
//	-graph-dir   var/entity-graph     (reads signals, writes new signals)
//	-out-dir     var/form4            (writes transactions.ndjson)
//	-lookback    90                   (days of EDGAR history per ticker)
//	-poll-interval 6h                 (interval between polls; ignored in -one-shot)
//	-one-shot                         (exit after one cycle; for cron/systemd)
//	-dry-run                          (log findings without writing files)
//
// Env:
//
//	FATBABY_ROOT   root of the fatbaby repo (default ".")
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/example/prrject-fatbaby/internal/entitygraph"
	"github.com/example/prrject-fatbaby/internal/insider"
	"github.com/example/prrject-fatbaby/secwatch"
)

const (
	submissionsBaseURL = "https://data.sec.gov/submissions/"
	userAgent          = "prrject-fatbaby/form4-watcher contact@example.com"
)

func main() {
	watchlistPath := flag.String("watchlist", filepath.Join("config", "watchlist.json"), "watchlist JSON path")
	graphDir := flag.String("graph-dir", filepath.Join("var", "entity-graph"), "entity-graph data directory")
	outDir := flag.String("out-dir", filepath.Join("var", "form4"), "output directory for transactions.ndjson")
	lookback := flag.Int("lookback", 90, "days of EDGAR history per ticker")
	pollInterval := flag.Duration("poll-interval", 6*time.Hour, "interval between polls")
	oneShot := flag.Bool("one-shot", false, "exit after one cycle")
	dryRun := flag.Bool("dry-run", false, "log findings without writing files")
	flag.Parse()

	logger := log.New(os.Stdout, "form4-watcher ", log.LstdFlags|log.LUTC)

	if !*dryRun {
		for _, dir := range []string{*outDir, *graphDir} {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				logger.Fatalf("mkdir %s: %v", dir, err)
			}
		}
	}

	client := &http.Client{Timeout: 20 * time.Second}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	cfg := pollConfig{
		watchlistPath: *watchlistPath,
		graphDir:      *graphDir,
		outDir:        *outDir,
		lookbackDays:  *lookback,
		dryRun:        *dryRun,
	}

	for {
		if err := runPoll(ctx, logger, client, cfg); err != nil {
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
	watchlistPath string
	graphDir      string
	outDir        string
	lookbackDays  int
	dryRun        bool
}

func runPoll(ctx context.Context, logger *log.Logger, client *http.Client, cfg pollConfig) error {
	wl, err := secwatch.LoadWatchlist(cfg.watchlistPath)
	if err != nil {
		return fmt.Errorf("load watchlist: %w", err)
	}

	since := time.Now().UTC().AddDate(0, 0, -cfg.lookbackDays)

	// Load existing transactions so we can de-duplicate by accession number.
	seen, err := loadSeenAccessions(cfg.outDir)
	if err != nil {
		logger.Printf("warn: could not load seen accessions: %v (proceeding without dedup)", err)
		seen = map[string]bool{}
	}

	var allNew []insider.InsiderTransaction
	var allSignals []entitygraph.Signal

	for _, entry := range wl.Entries {
		if !entry.Enabled {
			continue
		}
		txns, err := fetchForm4s(ctx, client, entry.Ticker, entry.CIK, since)
		if err != nil {
			logger.Printf("fetch ticker=%s err=%v (skipping)", entry.Ticker, err)
			continue
		}

		// De-duplicate by accession number.
		var newTxns []insider.InsiderTransaction
		for _, tx := range txns {
			if !seen[tx.AccessionNumber] {
				newTxns = append(newTxns, tx)
				seen[tx.AccessionNumber] = true
			}
		}
		if len(newTxns) == 0 {
			continue
		}
		logger.Printf("ticker=%s new_transactions=%d", entry.Ticker, len(newTxns))
		allNew = append(allNew, newTxns...)

		// Score signals from all transactions for this ticker (historical + new).
		allForTicker := append(loadTickerTransactions(cfg.outDir, entry.Ticker), newTxns...)
		sigs := insider.ScoreInsiderActivity(allForTicker, entry.Ticker)
		allSignals = append(allSignals, sigs...)
	}

	if len(allNew) == 0 {
		logger.Printf("poll complete: no new Form 4 transactions (lookback=%dd)", cfg.lookbackDays)
		return nil
	}

	logger.Printf("poll complete: %d new transactions, %d signals", len(allNew), len(allSignals))

	if cfg.dryRun {
		for _, tx := range allNew {
			logger.Printf("  [dry-run] %s %s code=%s shares=%.0f price=%.2f owner=%q role=%s",
				tx.Ticker, tx.TransactionDate, tx.Code, tx.Shares, tx.PricePerShare, tx.OwnerName, tx.Role)
		}
		return nil
	}

	if err := writeTransactions(cfg.outDir, allNew); err != nil {
		return fmt.Errorf("write transactions: %w", err)
	}

	if len(allSignals) > 0 {
		if err := entitygraph.WriteSignals(cfg.graphDir, allSignals); err != nil {
			logger.Printf("write signals err=%v", err)
		} else {
			for _, s := range allSignals {
				logger.Printf("signal type=%s ticker=%s severity=%s", s.Type, s.Ticker, s.Severity)
			}
		}
	}
	return nil
}

// ── EDGAR submissions API ─────────────────────────────────────────────────────

// submissionsResult is a minimal parse of CIK{n}.json from data.sec.gov.
type submissionsResult struct {
	Filings struct {
		Recent struct {
			AccessionNumber []string `json:"accessionNumber"`
			Form            []string `json:"form"`
			FilingDate      []string `json:"filingDate"`
			PrimaryDocument []string `json:"primaryDocument"`
		} `json:"recent"`
	} `json:"filings"`
}

// fetchForm4s queries EDGAR's submissions API for Form 4 filings for the given
// CIK since the given date, fetches each Form 4 XML document, and returns all
// parsed InsiderTransactions.
func fetchForm4s(ctx context.Context, client *http.Client, ticker, cik string, since time.Time) ([]insider.InsiderTransaction, error) {
	normCIK := secwatch.NormalizeCIK(cik)
	subURL := submissionsBaseURL + "CIK" + normCIK + ".json"

	body, err := getJSON(ctx, client, subURL)
	if err != nil {
		return nil, fmt.Errorf("submissions %s: %w", ticker, err)
	}

	var sub submissionsResult
	if err := json.Unmarshal(body, &sub); err != nil {
		return nil, fmt.Errorf("parse submissions %s: %w", ticker, err)
	}

	r := sub.Filings.Recent
	n := len(r.AccessionNumber)

	var all []insider.InsiderTransaction
	for i := 0; i < n; i++ {
		if strings.ToUpper(strings.TrimSpace(r.Form[i])) != "4" {
			continue
		}
		filingDate := strings.TrimSpace(r.FilingDate[i])
		fd, err := time.Parse("2006-01-02", filingDate)
		if err != nil || fd.Before(since) {
			continue
		}
		accession := strings.TrimSpace(r.AccessionNumber[i])
		primaryDoc := strings.TrimSpace(r.PrimaryDocument[i])
		if primaryDoc == "" {
			continue
		}
		// EDGAR sometimes prefixes the document path with an XSL stylesheet
		// directory (e.g. "xslF345X06/form4.xml"). That URL returns rendered
		// HTML, not the raw XML we need. Strip the leading xsl*/ component.
		if idx := strings.Index(primaryDoc, "/"); idx > 0 && strings.HasPrefix(primaryDoc, "xsl") {
			primaryDoc = primaryDoc[idx+1:]
		}

		docURL := secwatch.DocumentURL(cik, accession, primaryDoc)
		xmlBytes, err := getJSONWithRetry(ctx, client, docURL)
		if err != nil {
			// Non-fatal: skip this filing.
			continue
		}

		txns, err := insider.ParseForm4XML(xmlBytes, ticker, normCIK, accession, filingDate)
		if err != nil {
			// Parsing errors are common for amended/corrected filings; skip.
			continue
		}
		all = append(all, txns...)

		// Respect EDGAR rate limit: max 10 req/s; we conservatively do 5.
		select {
		case <-ctx.Done():
			return all, nil
		case <-time.After(200 * time.Millisecond):
		}
	}
	return all, nil
}

func getJSON(ctx context.Context, client *http.Client, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
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
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(b))
	}
	return io.ReadAll(io.LimitReader(resp.Body, 32<<20))
}

// getJSONWithRetry wraps getJSON with a single 429-aware retry (30s backoff).
// Used for individual Form 4 XML fetches; EDGAR rate-limits aggressively.
func getJSONWithRetry(ctx context.Context, client *http.Client, rawURL string) ([]byte, error) {
	b, err := getJSON(ctx, client, rawURL)
	if err == nil {
		return b, nil
	}
	if !strings.Contains(err.Error(), "HTTP 429") {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(30 * time.Second):
	}
	return getJSON(ctx, client, rawURL)
}

// ── Persistence ───────────────────────────────────────────────────────────────

func writeTransactions(outDir string, txns []insider.InsiderTransaction) error {
	p := filepath.Join(outDir, "transactions.ndjson")
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for i := range txns {
		if err := enc.Encode(&txns[i]); err != nil {
			return err
		}
	}
	return nil
}

// loadSeenAccessions reads transactions.ndjson and returns a set of accession
// numbers already recorded, used to prevent duplicates across poll cycles.
func loadSeenAccessions(outDir string) (map[string]bool, error) {
	p := filepath.Join(outDir, "transactions.ndjson")
	f, err := os.Open(p)
	if os.IsNotExist(err) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	seen := map[string]bool{}
	dec := json.NewDecoder(f)
	for {
		var tx insider.InsiderTransaction
		if err := dec.Decode(&tx); err != nil {
			break
		}
		if tx.AccessionNumber != "" {
			seen[tx.AccessionNumber] = true
		}
	}
	return seen, nil
}

// loadTickerTransactions reads all transactions for a specific ticker from
// transactions.ndjson. Used to re-score signals including historical data.
func loadTickerTransactions(outDir, ticker string) []insider.InsiderTransaction {
	p := filepath.Join(outDir, "transactions.ndjson")
	f, err := os.Open(p)
	if err != nil {
		return nil
	}
	defer f.Close()

	up := strings.ToUpper(ticker)
	var out []insider.InsiderTransaction
	dec := json.NewDecoder(f)
	for {
		var tx insider.InsiderTransaction
		if err := dec.Decode(&tx); err != nil {
			break
		}
		if strings.ToUpper(tx.Ticker) == up {
			out = append(out, tx)
		}
	}
	return out
}

// padCIK is kept for any EFTS usage (not used in this watcher but kept for parity).
func padCIK(cik string) string {
	digits := ""
	for _, r := range cik {
		if r >= '0' && r <= '9' {
			digits += string(r)
		}
	}
	for len(digits) < 10 {
		digits = "0" + digits
	}
	return digits
}

