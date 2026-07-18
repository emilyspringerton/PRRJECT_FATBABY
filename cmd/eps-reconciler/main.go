// eps-reconciler scans the secwatch event store for 8-K source documents
// (Item 2.02 — Results of Operations) and reconciles the filed EPS against
// pending oracle cases in var/eps/oracle.ndjson.
//
// When a match is found:
//   - Filed EPS agrees with extracted EPS (within tolerance) → VerdictConfirmed
//   - Filed EPS differs                                       → VerdictContradicts
//
// The oracle file is rewritten atomically after each batch so that precision
// tracking is always up to date for the Emily/Claude feedback loop.
//
// Run in one-shot mode for cron; omit -one-shot for continuous polling.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/example/prrject-fatbaby/internal/entitygraph"
	"github.com/example/prrject-fatbaby/internal/eps"
	"github.com/example/prrject-fatbaby/pkg/intelligence"
)

const (
	// epsTolerance is the relative tolerance for EPS match (5%).
	// A filed EPS within 5% of the extracted EPS is considered confirmed.
	epsTolerance = 0.05
)

func main() {
	storeRoot := flag.String("store", filepath.Join("var", "secwatch"), "secwatch event store root (source_document_persisted 8-K events)")
	epsDir := flag.String("eps-dir", filepath.Join("var", "eps"), "directory containing oracle.ndjson (read/write)")
	graphDir := flag.String("graph-dir", filepath.Join("var", "entity-graph"), "entity-graph data directory (writes signals for filing revisions)")
	pollInterval := flag.Duration("poll-interval", 6*time.Hour, "how often to re-scan the secwatch store")
	oneShot := flag.Bool("one-shot", false, "run one reconcile pass and exit")
	flag.Parse()

	logger := log.New(os.Stdout, "eps-reconciler ", log.LstdFlags|log.LUTC)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	store, err := eventstore.NewFileStore(*storeRoot)
	if err != nil {
		logger.Fatalf("open secwatch store %s: %v", *storeRoot, err)
	}
	defer store.Close()

	logger.Printf("starting poll_interval=%s store=%s eps_dir=%s graph_dir=%s", *pollInterval, *storeRoot, *epsDir, *graphDir)

	for {
		reconcile(ctx, store, *epsDir, *graphDir, logger)

		if *oneShot {
			return
		}
		select {
		case <-ctx.Done():
			logger.Printf("shutting down")
			return
		case <-time.After(*pollInterval):
		}
	}
}

func reconcile(ctx context.Context, store eventstore.EventStore, epsDir, graphDir string, logger *log.Logger) {
	cases, err := eps.LoadOracleCases(epsDir)
	if err != nil {
		logger.Printf("load oracle cases: %v", err)
		return
	}

	pendingByKey := map[string]*eps.OracleCase{}
	for i := range cases {
		if cases[i].Verdict == eps.VerdictPending {
			k := caseKey(cases[i].Ticker, cases[i].Period)
			pendingByKey[k] = &cases[i]
		}
	}

	if len(pendingByKey) == 0 {
		logger.Printf("no pending oracle cases")
		return
	}
	logger.Printf("reconciling %d pending cases", len(pendingByKey))

	// Scan the secwatch store for 8-K source documents.
	confirmed := 0
	contradicts := 0
	scanErr := store.Scan(ctx, 1, func(rec eventstore.Record) error {
		if rec.Event.Type != "source_document_persisted" {
			return nil
		}
		var doc intelligence.SourceDocument
		if err := json.Unmarshal(rec.Event.Data, &doc); err != nil {
			return nil
		}
		// Only care about 8-K filings that likely contain earnings data.
		if doc.Form != "8-K" {
			return nil
		}
		if !isEarningsRelease(doc.CleanedText) {
			return nil
		}

		// Extract EPS from the 8-K.
		filed := eps.Extract(doc.CleanedText, doc.Identity, doc.Ticker)
		if filed.EPSGAAPDiluted == nil {
			return nil
		}
		filedEPS := *filed.EPSGAAPDiluted

		// Try to match against a pending case by ticker + period.
		k := caseKey(doc.Ticker, filed.Period)
		c, ok := pendingByKey[k]
		if !ok {
			return nil
		}
		if c.ExtractedEPS == nil {
			return nil
		}

		filedVal := filedEPS
		c.FiledEPS = &filedVal
		c.FiledAccession = doc.Identity

		now := time.Now().UTC().Format("2006-01-02")
		extracted := *c.ExtractedEPS
		delta := filedEPS - extracted
		c.Delta = &delta
		c.RecordedAt = now

		relErr := 0.0
		if extracted != 0 {
			relErr = math.Abs(delta) / math.Abs(extracted)
		}

		if relErr <= epsTolerance {
			c.Verdict = eps.VerdictConfirmed
			confirmed++
			logger.Printf("confirmed ticker=%q period=%s extracted=%.2f filed=%.2f",
				c.Ticker, c.Period.FiscalQuarter, extracted, filedEPS)
		} else {
			c.Verdict = eps.VerdictContradicts
			c.Notes = "filed EPS differs from press release extraction"
			contradicts++
			logger.Printf("CONTRADICTS ticker=%q period=%s extracted=%.2f filed=%.2f delta=%.4f",
				c.Ticker, c.Period.FiscalQuarter, extracted, filedEPS, delta)
			// Emit an entity-graph signal when the filed EPS materially
			// differs from the press release EPS. This can indicate a
			// GAAP/non-GAAP labelling mismatch, a genuine revision between
			// the PR and the filing, or an extraction error worth reviewing.
			if graphDir != "" {
				sig := scoreEPSRevision(c.Ticker, c.Period.FiscalQuarter, extracted, filedEPS, delta, doc.FilingDate)
				if err := entitygraph.WriteSignals(graphDir, []entitygraph.Signal{sig}); err != nil {
					logger.Printf("write eps_filing_revision signal: %v", err)
				}
			}
		}
		delete(pendingByKey, k)
		return nil
	})
	if scanErr != nil {
		logger.Printf("scan secwatch store: %v", scanErr)
		return
	}

	if confirmed+contradicts == 0 {
		logger.Printf("no matches found against pending cases")
		return
	}

	// Rewrite oracle with updated verdicts.
	if err := eps.WriteOracleCases(epsDir, cases); err != nil {
		logger.Printf("write oracle cases: %v", err)
		return
	}

	summary := eps.BuildOracleSummary(cases)
	logger.Printf("reconcile done: confirmed=%d contradicts=%d total=%d pending=%d precision=%.3f",
		confirmed, contradicts, summary.Total, summary.Pending, summary.Precision)
}

// isEarningsRelease returns true when the 8-K cleaned text looks like an
// Item 2.02 earnings release (contains EPS-related language).
func isEarningsRelease(text string) bool {
	norm := strings.ToLower(text)
	return (strings.Contains(norm, "item 2.02") ||
		strings.Contains(norm, "results of operations") ||
		strings.Contains(norm, "earnings per share") ||
		strings.Contains(norm, "diluted earnings")) &&
		strings.Contains(norm, "per share")
}

// caseKey builds a lookup key from ticker + fiscal quarter + fiscal year.
// Quarter+year is used (not period_end) because the 8-K and press release
// may format the period-end date differently.
func caseKey(ticker string, period eps.EarningsPeriod) string {
	yearStr := period.PeriodEnd
	if len(yearStr) >= 4 {
		yearStr = yearStr[:4]
	} else if period.FiscalYear > 0 {
		yearStr = fmt.Sprintf("%d", period.FiscalYear)
	}
	return ticker + ":" + period.FiscalQuarter + ":" + yearStr
}

// scoreEPSRevision creates a late_filing_revision signal when the 8-K EPS
// materially differs from what was announced in the press release.
func scoreEPSRevision(ticker, quarter string, extracted, filed, delta float64, filingDate string) entitygraph.Signal {
	today := time.Now().UTC().Format("2006-01-02")
	if filingDate == "" {
		filingDate = today
	}
	pctDiff := 0.0
	if extracted != 0 {
		pctDiff = math.Abs(delta) / math.Abs(extracted) * 100
	}
	direction := "higher"
	if delta < 0 {
		direction = "lower"
	}
	interp := fmt.Sprintf("%s %s 8-K filed EPS (%.2f) is %.1f%% %s than press release EPS (%.2f) for %s. Possible GAAP/non-GAAP mislabel in press release, genuine post-announcement revision, or extraction error. Review the filed document.",
		ticker, filingDate, filed, pctDiff, direction, extracted, quarter)
	sev := entitygraph.SeverityMedium
	if pctDiff > 20 {
		sev = entitygraph.SeverityHigh
	}
	return entitygraph.Signal{
		SignalID:       fmt.Sprintf("eps_filing_revision_%s_%s_%s", strings.ToLower(ticker), quarter, strings.ReplaceAll(filingDate, "-", "")),
		Type:           entitygraph.SignalEPSFilingRevision,
		Ticker:         ticker,
		Severity:       sev,
		Confidence:     0.75,
		Score:          math.Min(pctDiff/50.0, 1.0),
		DetectedAt:     today,
		FilingDate:     filingDate,
		ValidThrough:   time.Now().UTC().AddDate(0, 6, 0).Format("2006-01-02"),
		Interpretation: interp,
		Metadata: map[string]string{
			"quarter":        quarter,
			"extracted_eps":  fmt.Sprintf("%.2f", extracted),
			"filed_eps":      fmt.Sprintf("%.2f", filed),
			"delta":          fmt.Sprintf("%.4f", delta),
			"pct_difference": fmt.Sprintf("%.1f", pctDiff),
		},
	}
}
