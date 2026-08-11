// cmd/market-data-watcher fetches daily OHLCV (open/high/low/close/adj_close/volume)
// from the Yahoo Finance v8 chart API for every enabled ticker on the watchlist.
// It emits market_data_tick events to var/market-data/ (a standard FatBaby eventstore)
// so downstream systems (projector, entity-graph, Emily) can correlate price movements
// with governance signals.
//
// On the first run per ticker (no cursor) it fetches -range of history.
// On subsequent runs it fetches only the last 5 days (incremental).
//
// Market cap enrichment: Yahoo's v8/finance/chart payload does NOT carry market
// cap or shares outstanding anywhere in its JSON (confirmed by inspecting a live
// response -- the `meta` object has price/range/exchange fields only). The Yahoo
// endpoints that DO carry marketCap (v7/finance/quote, v10/finance/quoteSummary,
// the POST-based v1/finance/screener) all require a session crumb and return 401
// Unauthorized without one -- not worth the fragility of a cookie/crumb dance for
// one field. Instead each ticker's shares-outstanding is pulled from SEC EDGAR's
// company-facts API (data.sec.gov/api/xbrl/companyconcept, dei tag
// EntityCommonStockSharesOutstanding) -- free, unauthenticated, and already the
// same host/politeness family cmd/secwatch talks to. Shares outstanding barely
// moves day to day (it's a per-filing cover-page number), so it's cached to disk
// per ticker and only refetched once -shares-refresh-interval has elapsed --
// steady state is about one SEC request per ticker per week, piggybacked onto
// the SAME per-ticker loop iteration (and its existing -request-delay pacing)
// this file already runs for the Yahoo OHLCV fetch, not a separate unpaced path.
// market_data_tick events then carry market_cap = latest close * cached shares
// outstanding.
//
// Flags:
//
//	-watchlist      config/watchlist.json
//	-out-dir        var/market-data        (eventstore root)
//	-cursor-dir     var/market-data-watcher  (per-ticker last-date cursor files)
//	-range          2y     (Yahoo range param on first/backfill run; values: 1y 2y 5y max)
//	-poll-interval  24h    (interval between full poll cycles)
//	-request-delay  600ms  (delay between individual ticker HTTP requests)
//	-shares-refresh-interval  168h  (min time between SEC shares-outstanding refetches, per ticker)
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
	incrementalRange = "5d"
	userAgent      = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 Chrome/120.0.0.0 Safari/537.36"

	secUserAgent = "prrject-fatbaby-market-data-watcher/0.1 (contact: secops@example.com)"

	// defaultSharesRefreshInterval is how stale a cached shares-outstanding
	// value can get before we spend one more SEC request refreshing it.
	// Shares outstanding is a cover-page number that only changes when a new
	// 10-K/10-Q/8-K is filed, so weekly is generous, not aggressive.
	defaultSharesRefreshInterval = 7 * 24 * time.Hour
)

// yahooBaseURL and secCompanyConceptBaseURL are vars, not consts, so tests
// can point them at an httptest.Server (same pattern as internal/movers'
// screenerBaseURL).
var (
	yahooBaseURL = "https://query1.finance.yahoo.com/v8/finance/chart"
	// secCompanyConceptBaseURL is SEC EDGAR's XBRL company-facts-by-concept
	// API -- free, unauthenticated, no crumb. Same vendor/politeness family
	// cmd/secwatch already talks to (data.sec.gov), just a different path.
	secCompanyConceptBaseURL = "https://data.sec.gov/api/xbrl/companyconcept"
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
	sharesRefresh := flag.Duration("shares-refresh-interval", defaultSharesRefreshInterval, "min time between SEC shares-outstanding refetches, per ticker")
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
				entry.Ticker, entry.CIK, *cursorDir, *backfillRange, *sharesRefresh, *dryRun)
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

// fetchTicker fetches OHLCV data for a single ticker, enriches it with market
// cap (price * cached shares-outstanding), emits new events to the store, and
// advances the cursor. Returns the number of events emitted.
func fetchTicker(
	ctx context.Context,
	client *http.Client,
	logger *log.Logger,
	store eventstore.EventStore,
	ticker, cik, cursorDir, backfillRange string,
	sharesRefreshInterval time.Duration,
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

	// Shares-outstanding refresh: gated by cache freshness, not by whether
	// there are new price bars, so it stays on schedule even for a ticker
	// with no new trading days. This is the one extra network call this
	// file makes beyond the existing Yahoo OHLCV fetch -- it happens inside
	// the same per-ticker loop iteration as that fetch, so it inherits the
	// caller's existing -request-delay pacing between tickers, and in
	// steady state fires at most once every sharesRefreshInterval per
	// ticker (default weekly), never once per poll cycle.
	sharesPath := filepath.Join(cursorDir, cursorKey(ticker)+".shares.json")
	sharesOut := refreshSharesOutstanding(ctx, client, logger, ticker, cik, sharesPath, sharesRefreshInterval, dryRun)

	if sharesOut > 0 {
		for i := range newBars {
			newBars[i].MarketCap = int64(newBars[i].Close * float64(sharesOut))
		}
	}

	if len(newBars) == 0 {
		return 0, nil
	}

	if dryRun {
		for _, b := range newBars {
			logger.Printf("[dry-run] ticker=%s date=%s close=%.4f vol=%d market_cap=%d", ticker, b.Date, b.Close, b.Volume, b.MarketCap)
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
	// MarketCap is close * shares-outstanding-as-cached-at-fetch-time (see
	// refreshSharesOutstanding). 0 when shares outstanding isn't known yet
	// (e.g. very first cycle for a ticker, before the SEC fetch has ever
	// succeeded, or the ticker has no CIK on the watchlist).
	MarketCap int64 `json:"market_cap,omitempty"`
}

// sharesCache is the on-disk cache of one ticker's most recently fetched
// shares-outstanding figure, so market cap survives a process restart
// without needing to re-hit SEC, and so the freshness check in
// refreshSharesOutstanding has something to compare against.
type sharesCache struct {
	SharesOutstanding int64     `json:"shares_outstanding"`
	AsOfDate          string    `json:"as_of_date"` // the XBRL fact's "end" date, informational
	FetchedAt         time.Time `json:"fetched_at"`
}

// refreshSharesOutstanding returns the best known shares-outstanding count
// for ticker, refetching from SEC only if the cache is missing or older than
// refreshInterval. Any SEC error is logged and swallowed -- market cap is
// supplementary enrichment, never worth failing the ticker's whole OHLCV
// fetch over. Returns 0 if no value is known yet (no cache, no CIK, or the
// first-ever SEC fetch hasn't succeeded).
func refreshSharesOutstanding(
	ctx context.Context,
	client *http.Client,
	logger *log.Logger,
	ticker, cik, cachePath string,
	refreshInterval time.Duration,
	dryRun bool,
) int64 {
	cached, _ := loadSharesCache(cachePath)
	if cached != nil && time.Since(cached.FetchedAt) < refreshInterval {
		return cached.SharesOutstanding
	}
	if strings.TrimSpace(cik) == "" {
		if cached != nil {
			return cached.SharesOutstanding
		}
		return 0
	}

	shares, asOf, err := fetchSharesOutstandingSEC(ctx, client, cik)
	if err != nil {
		logger.Printf("ticker=%s cik=%s shares-outstanding fetch failed (using cached/zero): %v", ticker, cik, err)
		if cached != nil {
			return cached.SharesOutstanding
		}
		return 0
	}

	fresh := sharesCache{SharesOutstanding: shares, AsOfDate: asOf, FetchedAt: time.Now().UTC()}
	if !dryRun {
		if err := saveSharesCache(cachePath, fresh); err != nil {
			logger.Printf("ticker=%s warn: save shares cache: %v", ticker, err)
		}
	}
	return shares
}

func loadSharesCache(path string) (*sharesCache, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c sharesCache
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func saveSharesCache(path string, c sharesCache) error {
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// secCompanyConceptResponse is the shape of SEC EDGAR's
// data.sec.gov/api/xbrl/companyconcept/CIK{10-digit}/{taxonomy}/{tag}.json
// response.
type secCompanyConceptResponse struct {
	Units struct {
		Shares []struct {
			End string `json:"end"`
			Val int64  `json:"val"`
		} `json:"shares"`
	} `json:"units"`
}

// fetchSharesOutstandingSEC fetches the dei:EntityCommonStockSharesOutstanding
// concept for cik from SEC EDGAR's company-facts API and returns the most
// recent reported value (the last entry in the "shares" series, which SEC
// returns in filing order). This is a cover-page XBRL fact refreshed once
// per 10-K/10-Q/8-K, not a live quote -- exactly why it's safe to cache for
// days at a time.
func fetchSharesOutstandingSEC(ctx context.Context, client *http.Client, cik string) (int64, string, error) {
	normalized := normalizeCIKForSEC(cik)
	if normalized == "" {
		return 0, "", fmt.Errorf("empty/invalid cik")
	}
	url := fmt.Sprintf("%s/CIK%s/dei/EntityCommonStockSharesOutstanding.json", secCompanyConceptBaseURL, normalized)

	resp, err := httpretry.Do(ctx, httpretry.Options{MaxRetries: 2, BackoffBase: 500 * time.Millisecond, BackoffCap: 8 * time.Second},
		func(ctx context.Context, attempt int) (secCompanyConceptResponse, error) {
			var out secCompanyConceptResponse
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				return out, err
			}
			req.Header.Set("User-Agent", secUserAgent)
			req.Header.Set("Accept", "application/json")

			r, err := client.Do(req)
			if err != nil {
				return out, err
			}
			defer r.Body.Close()

			if r.StatusCode != http.StatusOK {
				return out, &httpretry.StatusError{StatusCode: r.StatusCode, URL: url}
			}
			if err := json.NewDecoder(r.Body).Decode(&out); err != nil {
				return out, fmt.Errorf("decode json: %w", err)
			}
			return out, nil
		})
	if err != nil {
		return 0, "", fmt.Errorf("fetch shares outstanding cik=%s after retries: %w", normalized, err)
	}

	n := len(resp.Units.Shares)
	if n == 0 {
		return 0, "", fmt.Errorf("no shares-outstanding facts for cik=%s", normalized)
	}
	latest := resp.Units.Shares[n-1]
	if latest.Val <= 0 {
		return 0, "", fmt.Errorf("non-positive shares-outstanding value for cik=%s", normalized)
	}
	return latest.Val, latest.End, nil
}

// normalizeCIKForSEC zero-pads a CIK to SEC's 10-digit path format. The
// watchlist already stores CIKs zero-padded via secwatch.NormalizeCIK, but
// this is defensive against a raw/short CIK reaching this function directly.
func normalizeCIKForSEC(cik string) string {
	digits := make([]rune, 0, len(cik))
	for _, r := range cik {
		if r >= '0' && r <= '9' {
			digits = append(digits, r)
		}
	}
	if len(digits) == 0 {
		return ""
	}
	d := string(digits)
	if len(d) >= 10 {
		return d
	}
	return strings.Repeat("0", 10-len(d)) + d
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

