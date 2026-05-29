// schd13-watcher polls SEC EDGAR for Schedule 13D and 13G filings targeting
// companies on the watchlist. When a filing is found it is appended to
// var/schd13/filings.ndjson. On each run it also correlates newly discovered
// filings against existing activist_risk signals in var/entity-graph/signals.ndjson
// and appends accuracy records to var/entity-graph/accuracy.ndjson.
//
// Key flags:
//
//	-watchlist  config/watchlist.json (reads ticker+CIK pairs)
//	-graph-dir  var/entity-graph      (reads signals.ndjson, writes accuracy.ndjson)
//	-out-dir    var/schd13            (appends filings.ndjson)
//	-lookback   90                    (days of EDGAR history to scan per ticker)
//	-one-shot   (exit after one poll cycle; for cron/systemd use)
//	-dry-run    (log found filings but do not write to disk)
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
)

const (
	// EDGAR EFTS full-text search endpoint.
	eftsSearchURL = "https://efts.sec.gov/LATEST/search-index"
	userAgent     = "prrject-fatbaby/schd13-watcher contact@example.com"
)

// watchlistEntry mirrors the structure in config/watchlist.json.
type watchlistEntry struct {
	Ticker  string `json:"ticker"`
	CIK     string `json:"cik"`
	Enabled bool   `json:"enabled"`
}

// watchlist is the top-level structure of config/watchlist.json.
type watchlist struct {
	Entries []watchlistEntry `json:"entries"`
}

// eftsHit is one search result from the EDGAR EFTS API.
type eftsHit struct {
	Source struct {
		FileDate      string `json:"file_date"`
		FormType      string `json:"form_type"`
		EntityName    string `json:"entity_name"`
		AccessionNo   string `json:"accession_no"`
		DisplayNames  []string `json:"display_names"`
	} `json:"_source"`
}

// eftsResponse is the top-level shape returned by EDGAR EFTS.
type eftsResponse struct {
	Hits struct {
		Total struct {
			Value int `json:"value"`
		} `json:"total"`
		Hits []eftsHit `json:"hits"`
	} `json:"hits"`
}

func main() {
	watchlistPath := flag.String("watchlist", filepath.Join("config", "watchlist.json"), "path to watchlist.json")
	graphDir := flag.String("graph-dir", filepath.Join("var", "entity-graph"), "entity-graph data directory (reads signals, writes accuracy)")
	outDir := flag.String("out-dir", filepath.Join("var", "schd13"), "output directory for filings.ndjson")
	lookback := flag.Int("lookback", 90, "days of EDGAR history to scan per ticker")
	pollInterval := flag.Duration("poll-interval", 6*time.Hour, "interval between EDGAR polls (not used in one-shot mode)")
	oneShot := flag.Bool("one-shot", false, "exit after one poll cycle")
	dryRun := flag.Bool("dry-run", false, "log filings found but do not write to disk")
	flag.Parse()

	logger := log.New(os.Stdout, "schd13-watcher ", log.LstdFlags|log.LUTC)

	if !*dryRun {
		if err := os.MkdirAll(*outDir, 0o755); err != nil {
			logger.Fatalf("mkdir %s: %v", *outDir, err)
		}
	}

	client := &http.Client{Timeout: 20 * time.Second}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	for {
		if err := runPoll(ctx, logger, client, runPollConfig{
			watchlistPath: *watchlistPath,
			graphDir:      *graphDir,
			outDir:        *outDir,
			lookbackDays:  *lookback,
			dryRun:        *dryRun,
		}); err != nil {
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

type runPollConfig struct {
	watchlistPath string
	graphDir      string
	outDir        string
	lookbackDays  int
	dryRun        bool
}

func runPoll(ctx context.Context, logger *log.Logger, client *http.Client, cfg runPollConfig) error {
	wl, err := loadWatchlist(cfg.watchlistPath)
	if err != nil {
		return fmt.Errorf("load watchlist: %w", err)
	}

	since := time.Now().UTC().AddDate(0, 0, -cfg.lookbackDays).Format("2006-01-02")
	var newFilings []entitygraph.Schd13Filing

	for _, entry := range wl.Entries {
		if !entry.Enabled {
			continue
		}
		filings, err := fetchSchd13(ctx, client, entry, since)
		if err != nil {
			logger.Printf("fetch ticker=%s err=%v (skipping)", entry.Ticker, err)
			continue
		}
		if len(filings) > 0 {
			logger.Printf("found ticker=%s filings=%d", entry.Ticker, len(filings))
		}
		newFilings = append(newFilings, filings...)
	}

	if len(newFilings) == 0 {
		logger.Printf("poll complete: no new 13D/13G filings found (lookback=%dd)", cfg.lookbackDays)
		return nil
	}

	logger.Printf("poll complete: found %d 13D/13G filings", len(newFilings))

	if cfg.dryRun {
		for _, f := range newFilings {
			logger.Printf("  [dry-run] %s %s filed=%s filer=%q", f.FilingType, f.Ticker, f.FilingDate, f.FilerName)
		}
		return nil
	}

	if err := entitygraph.WriteSchd13Filings(cfg.outDir, newFilings); err != nil {
		return fmt.Errorf("write schd13 filings: %w", err)
	}

	// Correlate new filings with activist_risk signals for accuracy tracking.
	allSignals, err := entitygraph.LoadSignals(cfg.graphDir)
	if err != nil {
		logger.Printf("load signals err=%v (skipping accuracy update)", err)
		return nil
	}
	allFilings, err := entitygraph.LoadSchd13Filings(cfg.outDir)
	if err != nil {
		logger.Printf("load all schd13 filings err=%v (skipping accuracy update)", err)
		return nil
	}
	accuracyRecords := entitygraph.CorrelateActivistRisk(allSignals, allFilings)
	if len(accuracyRecords) > 0 {
		if err := entitygraph.WriteAccuracyRecords(cfg.graphDir, accuracyRecords); err != nil {
			logger.Printf("write accuracy records err=%v", err)
		} else {
			reports := entitygraph.BuildAccuracyReports(accuracyRecords)
			for _, r := range reports {
				logger.Printf("accuracy signal_type=%s total=%d confirmed=%d refuted=%d pending=%d precision=%.2f",
					r.SignalType, r.TotalPredictions, r.Confirmed, r.Refuted, r.Pending, r.Precision)
			}
		}
	}
	return nil
}

// fetchSchd13 queries EDGAR EFTS for SC 13D/13G filings referencing a specific CIK
// since the given start date.
func fetchSchd13(ctx context.Context, client *http.Client, entry watchlistEntry, since string) ([]entitygraph.Schd13Filing, error) {
	// Search for filings that contain the target CIK in full text.
	params := url.Values{}
	params.Set("q", fmt.Sprintf(`"%s"`, padCIK(entry.CIK)))
	params.Set("forms", "SC 13D,SC 13G,SC 13D/A,SC 13G/A")
	params.Set("dateRange", "custom")
	params.Set("startdt", since)

	reqURL := eftsSearchURL + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP GET %s: %w", reqURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("EDGAR EFTS status %d: %s", resp.StatusCode, string(body))
	}

	var result eftsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode EFTS response: %w", err)
	}

	var filings []entitygraph.Schd13Filing
	for _, hit := range result.Hits.Hits {
		src := hit.Source
		filings = append(filings, entitygraph.Schd13Filing{
			Ticker:     entry.Ticker,
			TargetCIK:  entry.CIK,
			FilingDate: src.FileDate,
			FilingType: src.FormType,
			FilerName:  src.EntityName,
			Accession:  src.AccessionNo,
		})
	}
	return filings, nil
}

// padCIK returns the 10-digit zero-padded form of a CIK string.
func padCIK(cik string) string {
	cik = strings.TrimLeft(cik, "0")
	for len(cik) < 10 {
		cik = "0" + cik
	}
	return cik
}

func loadWatchlist(path string) (*watchlist, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var wl watchlist
	if err := json.Unmarshal(data, &wl); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &wl, nil
}
