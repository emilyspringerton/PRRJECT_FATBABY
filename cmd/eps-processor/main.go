// eps-processor polls the prwatch-body event store for pr_body_fetched events,
// runs EPS extraction + validation on each press release, and for publishable
// results writes an Article to var/eps/articles.ndjson and a pending OracleCase
// to var/eps/oracle.ndjson.
//
// It also reads pr_discovered events from the prwatch discovery store to resolve
// ticker symbols for each press release.
//
// On each run it advances a cursor at var/eps-processor/.cursor so only new
// events are processed on subsequent runs.
package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/example/prrject-fatbaby/internal/eps"
	"github.com/example/prrject-fatbaby/prwatch"
)

func main() {
	discoveryRoot := flag.String("discovery-store", filepath.Join("var", "prwatch"), "prwatch discovery event store root (pr_discovered events)")
	bodyRoot := flag.String("body-store", filepath.Join("var", "prwatch-body"), "prwatch body event store root (pr_body_fetched events)")
	epsDir := flag.String("eps-dir", filepath.Join("var", "eps"), "output directory for oracle.ndjson and articles.ndjson")
	pollInterval := flag.Duration("poll-interval", 30*time.Second, "how often to poll for new body events")
	cursorPath := flag.String("cursor", filepath.Join("var", "eps-processor", ".cursor"), "file storing last-processed body event sequence")
	batchSize := flag.Int("batch-size", 256, "max events to read per poll")
	oneShot := flag.Bool("one-shot", false, "process one batch and exit (useful for cron)")
	flag.Parse()

	logger := log.New(os.Stdout, "eps-processor ", log.LstdFlags|log.LUTC)

	if err := os.MkdirAll(*epsDir, 0o755); err != nil {
		logger.Fatalf("mkdir %s: %v", *epsDir, err)
	}
	if err := os.MkdirAll(filepath.Dir(*cursorPath), 0o755); err != nil {
		logger.Fatalf("mkdir cursor dir: %v", err)
	}

	discoveryStore, err := eventstore.NewFileStore(*discoveryRoot)
	if err != nil {
		logger.Fatalf("open discovery store %s: %v", *discoveryRoot, err)
	}
	defer discoveryStore.Close()

	bodyStore, err := eventstore.NewFileStore(*bodyRoot)
	if err != nil {
		logger.Fatalf("open body store %s: %v", *bodyRoot, err)
	}
	defer bodyStore.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	logger.Printf("starting poll_interval=%s body_store=%s eps_dir=%s", *pollInterval, *bodyRoot, *epsDir)

	cursor := loadCursor(*cursorPath, logger)

	for {
		// Refresh the discovery ticker map each batch (it grows over time as new
		// press releases are discovered; a full scan is cheap since it's append-only).
		tickerByDiscoveryID := loadTickerMap(ctx, discoveryStore, logger)
		if len(tickerByDiscoveryID) < 10 {
			logger.Printf("WARNING: ticker map has only %d entries — most press releases will be processed without a ticker symbol; EPS articles will lack ticker attribution", len(tickerByDiscoveryID))
		}

		cursor = runBatch(ctx, bodyStore, tickerByDiscoveryID, logger, batchConfig{
			epsDir:     *epsDir,
			cursorPath: *cursorPath,
			batchSize:  *batchSize,
			cursor:     cursor,
		})

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

type batchConfig struct {
	epsDir     string
	cursorPath string
	batchSize  int
	cursor     uint64
}

func runBatch(ctx context.Context, bodyStore eventstore.EventStore, tickerMap map[string]string, logger *log.Logger, cfg batchConfig) uint64 {
	recs, err := bodyStore.ReadFrom(ctx, cfg.cursor, cfg.batchSize)
	if err != nil {
		logger.Printf("read body store: %v", err)
		return cfg.cursor
	}
	if len(recs) == 0 {
		return cfg.cursor
	}

	published := 0
	skipped := 0

	for _, rec := range recs {
		if ctx.Err() != nil {
			break
		}
		if rec.Event.Type != "pr_body_fetched" {
			continue
		}

		var body prwatch.BodyFetchedEvent
		if err := json.Unmarshal(rec.Event.Data, &body); err != nil {
			logger.Printf("seq=%d unmarshal error: %v", rec.Sequence, err)
			continue
		}
		if body.Body == "" {
			continue
		}

		ticker := tickerMap[body.PRDiscoveryID]
		if ticker == "" {
			logger.Printf("seq=%d no_ticker discovery_id=%s — proceeding without ticker attribution", rec.Sequence, body.PRDiscoveryID)
		}
		sourceIdentity := "pr:" + body.PRDiscoveryID

		e := eps.Extract(body.Body, sourceIdentity, ticker)
		flags := eps.Validate(e, body.Body)

		val, _ := e.HeadlineEPS()
		if val == 0 {
			// No EPS found — not an earnings release.
			continue
		}

		art, ok := eps.Generate(e)
		if !ok {
			skipped++
			logger.Printf("seq=%d skip ticker=%q issuer=%q confidence=%.2f flags=%v",
				rec.Sequence, ticker, e.Issuer, e.ExtractionConfidence, flags)
			continue
		}

		art.SourceURL = body.URL

		if err := appendArticle(cfg.epsDir, art); err != nil {
			logger.Printf("seq=%d write article: %v", rec.Sequence, err)
			continue
		}

		delta := ""
		_ = delta
		now := time.Now().UTC().Format("2006-01-02")
		extracted := val
		oCase := eps.OracleCase{
			CaseID:         caseID(sourceIdentity, e.Period),
			SourceIdentity: sourceIdentity,
			Ticker:         ticker,
			Period:         e.Period,
			ExtractedEPS:   &extracted,
			Verdict:        eps.VerdictPending,
			RecordedAt:     now,
		}
		if err := eps.AppendOracleCase(cfg.epsDir, oCase); err != nil {
			logger.Printf("seq=%d write oracle case: %v", rec.Sequence, err)
			continue
		}

		logger.Printf("seq=%d published ticker=%q issuer=%q period=%s eps=%.2f headline=%q",
			rec.Sequence, ticker, e.Issuer, e.Period.FiscalQuarter+" "+fmt.Sprint(e.Period.FiscalYear), val, art.Headline)
		published++
	}

	newCursor := recs[len(recs)-1].Sequence + 1
	saveCursor(cfg.cursorPath, newCursor, logger)
	if published+skipped > 0 {
		logger.Printf("batch done: published=%d skipped=%d cursor=%d", published, skipped, newCursor)
	}
	return newCursor
}

// loadTickerMap reads all pr_discovered events and builds a map from
// discovery ID to the primary ticker symbol extracted at discovery time.
func loadTickerMap(ctx context.Context, store eventstore.EventStore, logger *log.Logger) map[string]string {
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
			// Discovery ID is stored in ev.Metadata["id"].
			if id, ok := ev.Metadata["id"]; ok {
				m[id] = ev.Identity.PrimaryTicker.Symbol
			}
		}
		return nil
	})
	if err != nil {
		logger.Printf("ticker map scan error: %v", err)
	}
	logger.Printf("ticker map loaded: %d entries", len(m))
	return m
}

// appendArticle appends one Article to <dir>/articles.ndjson.
func appendArticle(dir string, art eps.Article) error {
	path := filepath.Join(dir, "articles.ndjson")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	line, err := json.Marshal(art)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	if _, err := fmt.Fprintf(w, "%s\n", line); err != nil {
		return err
	}
	return w.Flush()
}

// caseID derives a stable case ID from source identity and period.
func caseID(sourceIdentity string, period eps.EarningsPeriod) string {
	h := sha256.Sum256([]byte(sourceIdentity + ":" + period.FiscalQuarter + ":" + fmt.Sprint(period.FiscalYear)))
	return fmt.Sprintf("eps:%x", h[:8])
}

func loadCursor(path string, logger *log.Logger) uint64 {
	data, err := os.ReadFile(path)
	if err != nil {
		return 1
	}
	var seq uint64
	if _, err := fmt.Sscan(string(data), &seq); err != nil {
		logger.Printf("bad cursor in %s: %v", path, err)
		return 1
	}
	return seq
}

func saveCursor(path string, seq uint64, logger *log.Logger) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		logger.Printf("cursor dir: %v", err)
		return
	}
	if err := os.WriteFile(path, []byte(fmt.Sprint(seq)), 0o644); err != nil {
		logger.Printf("save cursor: %v", err)
	}
}
