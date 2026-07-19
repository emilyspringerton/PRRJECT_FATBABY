// cmd/market-data-watcher fetches daily OHLCV (open/high/low/close/adj_close/volume)
// from the Yahoo Finance v8 chart API for every enabled ticker on the watchlist.
// It emits market_data_tick events to var/market-data/ (a standard FatBaby eventstore)
// so downstream systems (projector, entity-graph, Emily) can correlate price movements
// with governance signals.
//
// On the first run per ticker (no cursor) it fetches -range of history.
// On subsequent runs it fetches only the last 5 days (incremental).
//
// Flags:
//
//	-watchlist      config/watchlist.json
//	-out-dir        var/market-data        (eventstore root)
//	-cursor-dir     var/market-data-watcher  (per-ticker last-date cursor files)
//	-range          2y     (Yahoo range param on first/backfill run; values: 1y 2y 5y max)
//	-poll-interval  24h    (interval between full poll cycles)
//	-request-delay  600ms  (delay between individual ticker HTTP requests)
//	-one-shot              (exit after one cycle; for cron/systemd)
//	-dry-run               (log findings without writing)
//
// Env:
//
//	FATBABY_ROOT  root of the fatbaby repo (default ".")
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/example/prrject-fatbaby/internal/httpretry"
	"github.com/example/prrject-fatbaby/secwatch"
)

const (
	yahooBaseURL   = "https://query1.finance.yahoo.com/v8/finance/chart"
	incrementalRange = "5d"
	userAgent      = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 Chrome/120.0.0.0 Safari/537.36"
)

func main() {
	fatbabyRoot := os.Getenv("FATBABY_ROOT")
	if fatbabyRoot == "" {
		fatbabyRoot = "."
	}

	watchlistPath := flag.String("watchlist", filepath.Join(fatbabyRoot, "config", "watchlist.json"), "watchlist config")
	outDir        := flag.String("out-dir", filepath.Join(fatbabyRoot, "var", "market-data"), "eventstore root")
	cursorDir     := flag.String("cursor-dir", filepath.Join(fatbabyRoot, "var", "market-data-watcher"), "per-ticker cursor files")
	backfillRange := flag.String("range", "2y", "Yahoo Finance range for first/backfill fetch (1y|2y|5y|max)")
	pollInterval  := flag.Duration("poll-interval", 24*time.Hour, "interval between full poll cycles")
	reqDelay      := flag.Duration("request-delay", 600*time.Millisecond, "delay between ticker HTTP requests")
	oneShot       := flag.Bool("one-shot", false, "exit after one cycle")
	dryRun        := flag.Bool("dry-run", false, "log findings without writing")
	flag.Parse()

	logger := log.New(os.Stdout, "market-data-watcher ", log.LstdFlags|log.LUTC)

	for _, dir := range []string{*outDir, *cursorDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			logger.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	wl, err := secwatch.LoadWatchlist(*watchlistPath)
	if err != nil {
		logger.Fatalf("load watchlist: %v", err)
	}

	store, err := eventstore.NewFileStore(*outDir)
	if err != nil {
		logger.Fatalf("open eventstore: %v", err)
	}
	defer store.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client := &http.Client{Timeout: 20 * time.Second}

	for {
		entries := wl.EnabledEntries()
		logger.Printf("poll cycle: %d enabled tickers", len(entries))

		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				logger.Printf("shutdown during cycle")
				return
			}

			fetched, err := fetchTicker(ctx, client, logger, store,
				entry.Ticker, *cursorDir, *backfillRange, *dryRun)
			if err != nil {
				logger.Printf("ticker=%s error: %v", entry.Ticker, err)
			} else if fetched > 0 {
				logger.Printf("ticker=%s emitted=%d", entry.Ticker, fetched)
			}

			select {
			case <-ctx.Done():
				return
			case <-time.After(*reqDelay):
			}
		}

		if *oneShot {
			logger.Printf("one-shot complete")
			return
		}
		logger.Printf("cycle complete; sleeping %s", *pollInterval)
		select {
		case <-ctx.Done():
			return
		case <-time.After(*pollInterval):
		}
	}
}

// fetchTicker fetches OHLCV data for a single ticker, emits new events to the
// store, and advances the cursor. Returns the number of events emitted.
func fetchTicker(
	ctx context.Context,
	client *http.Client,
	logger *log.Logger,
	store eventstore.EventStore,
	ticker, cursorDir, backfillRange string,
	dryRun bool,
) (int, error) {
	cursorPath := filepath.Join(cursorDir, cursorKey(ticker)+".cursor")
	lastDate := loadCursor(cursorPath)

	// Use incremental range if we have a cursor; full backfill otherwise.
	fetchRange := backfillRange
	if lastDate != "" {
		fetchRange = incrementalRange
	}

	bars, err := fetchYahooOHLCV(ctx, client, ticker, fetchRange)
	if err != nil {
		return 0, fmt.Errorf("yahoo fetch: %w", err)
	}

	var newBars []OHLCVBar
	for _, b := range bars {
		if lastDate == "" || b.Date > lastDate {
			newBars = append(newBars, b)
		}
	}
	if len(newBars) == 0 {
		return 0, nil
	}

	if dryRun {
		for _, b := range newBars {
			logger.Printf("[dry-run] ticker=%s date=%s close=%.4f vol=%d", ticker, b.Date, b.Close, b.Volume)
		}
		return len(newBars), nil
	}

	events := make([]eventstore.Event, 0, len(newBars))
	for _, b := range newBars {
		data, _ := json.Marshal(b)
		events = append(events, eventstore.Event{
			ID:           fmt.Sprintf("market_data_tick:%s:%s", ticker, b.Date),
			Type:         "market_data_tick",
			Source:       "yahoo-finance",
			OccurredAt:   b.Timestamp,
			PartitionKey: ticker,
			Data:         data,
		})
	}

	if _, err := store.Append(ctx, events...); err != nil {
		return 0, fmt.Errorf("append events: %w", err)
	}

	// Advance cursor to the latest date we wrote.
	latest := newBars[len(newBars)-1].Date
	if err := saveCursor(cursorPath, latest); err != nil {
		logger.Printf("ticker=%s warn: save cursor: %v", ticker, err)
	}
	return len(newBars), nil
}

// OHLCVBar is the event data payload for a market_data_tick event.
type OHLCVBar struct {
	Ticker    string    `json:"ticker"`
	Date      string    `json:"date"`
	Timestamp time.Time `json:"timestamp"`
	Open      float64   `json:"open"`
	High      float64   `json:"high"`
	Low       float64   `json:"low"`
	Close     float64   `json:"close"`
	AdjClose  float64   `json:"adj_close"`
	Volume    int64     `json:"volume"`
}

// yahooChartResponse is the v8 chart API JSON envelope.
type yahooChartResponse struct {
	Chart struct {
		Result []struct {
			Meta struct {
				Symbol string `json:"symbol"`
			} `json:"meta"`
			Timestamp  []int64 `json:"timestamp"`
			Indicators struct {
				Quote []struct {
					Open   []*float64 `json:"open"`
					High   []*float64 `json:"high"`
					Low    []*float64 `json:"low"`
					Close  []*float64 `json:"close"`
					Volume []*int64   `json:"volume"`
				} `json:"quote"`
				AdjClose []struct {
					AdjClose []*float64 `json:"adjclose"`
				} `json:"adjclose"`
			} `json:"indicators"`
		} `json:"result"`
		Error *struct {
			Code        string `json:"code"`
			Description string `json:"description"`
		} `json:"error"`
	} `json:"chart"`
}

// fetchYahooOHLCV calls the Yahoo Finance v8 chart API and returns sorted
// bars, retrying transient failures (network errors, 429/403/5xx — Yahoo's
// unofficial API returns 403 for what's really rate limiting as often as it
// returns 429) with exponential backoff+jitter via internal/httpretry.
// Without this, one bad response for a ticker meant that ticker's data was
// simply missing until the next 24h cycle.
func fetchYahooOHLCV(ctx context.Context, client *http.Client, ticker, rangeStr string) ([]OHLCVBar, error) {
	sym := yahooSymbol(ticker)
	url := fmt.Sprintf("%s/%s?interval=1d&range=%s&includePrePost=false", yahooBaseURL, sym, rangeStr)

	chart, err := httpretry.Do(ctx, httpretry.Options{MaxRetries: 3, BackoffBase: 500 * time.Millisecond, BackoffCap: 8 * time.Second},
		func(ctx context.Context, attempt int) (yahooChartResponse, error) {
			var out yahooChartResponse
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				return out, err
			}
			req.Header.Set("User-Agent", userAgent)
			req.Header.Set("Accept", "application/json")

			resp, err := client.Do(req)
			if err != nil {
				return out, err // network error — treated as retryable by httpretry.IsRetryable
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				return out, &httpretry.StatusError{StatusCode: resp.StatusCode, URL: url}
			}
			if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
				return out, fmt.Errorf("decode json: %w", err) // not a StatusError -> IsRetryable treats malformed-body as retryable too, which is fine: a fresh request may succeed where a truncated one didn't
			}
			return out, nil
		})
	if err != nil {
		return nil, fmt.Errorf("fetch %s after retries: %w", sym, err)
	}

	if chart.Chart.Error != nil {
		return nil, fmt.Errorf("yahoo error %s: %s", chart.Chart.Error.Code, chart.Chart.Error.Description)
	}
	if len(chart.Chart.Result) == 0 {
		return nil, fmt.Errorf("no result for %s", sym)
	}

	result := chart.Chart.Result[0]
	if len(result.Timestamp) == 0 {
		return nil, nil
	}
	if len(result.Indicators.Quote) == 0 {
		return nil, fmt.Errorf("no quote data for %s", sym)
	}
	quote := result.Indicators.Quote[0]

	// adjclose is optional; fall back to close if missing.
	var adjCloseVals []*float64
	if len(result.Indicators.AdjClose) > 0 {
		adjCloseVals = result.Indicators.AdjClose[0].AdjClose
	}

	bars := make([]OHLCVBar, 0, len(result.Timestamp))
	for i, ts := range result.Timestamp {
		// Skip rows where any primary field is nil (incomplete data).
		if i >= len(quote.Open) || i >= len(quote.High) || i >= len(quote.Low) || i >= len(quote.Close) {
			continue
		}
		if quote.Open[i] == nil || quote.High[i] == nil || quote.Low[i] == nil || quote.Close[i] == nil {
			continue
		}
		vol := int64(0)
		if i < len(quote.Volume) && quote.Volume[i] != nil {
			vol = *quote.Volume[i]
		}
		adjClose := *quote.Close[i]
		if i < len(adjCloseVals) && adjCloseVals[i] != nil {
			adjClose = *adjCloseVals[i]
		}
		t := time.Unix(ts, 0).UTC()
		bars = append(bars, OHLCVBar{
			Ticker:    ticker,
			Date:      t.Format("2006-01-02"),
			Timestamp: t,
			Open:      *quote.Open[i],
			High:      *quote.High[i],
			Low:       *quote.Low[i],
			Close:     *quote.Close[i],
			AdjClose:  adjClose,
			Volume:    vol,
		})
	}
	return bars, nil
}

// yahooSymbol converts a watchlist ticker to the Yahoo Finance symbol format.
// BRK.A → BRK-A, etc.
func yahooSymbol(ticker string) string {
	return strings.ReplaceAll(ticker, ".", "-")
}

// cursorKey returns a filesystem-safe key for a ticker cursor file.
func cursorKey(ticker string) string {
	r := strings.NewReplacer(".", "_", "/", "_", "\\", "_", ":", "_")
	return strings.ToLower(r.Replace(ticker))
}

func loadCursor(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func saveCursor(path string, date string) error {
	return os.WriteFile(path, []byte(date+"\n"), 0o644)
}

