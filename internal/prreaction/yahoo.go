package prreaction

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/example/prrject-fatbaby/internal/httpretry"
)

const (
	yahooBaseURL = "https://query1.finance.yahoo.com/v8/finance/chart"
	userAgent    = "prrject-fatbaby-pr-reaction/0.1 (contact: secops@example.com)"
)

// yahooSymbol converts a watchlist ticker to Yahoo's symbol format (BRK.A -> BRK-A),
// same convention cmd/market-data-watcher's own yahooSymbol already uses.
func yahooSymbol(ticker string) string {
	return strings.ReplaceAll(ticker, ".", "-")
}

type yahooChartResponse struct {
	Chart struct {
		Result []struct {
			Timestamp  []int64 `json:"timestamp"`
			Indicators struct {
				Quote []struct {
					Close []*float64 `json:"close"`
				} `json:"quote"`
			} `json:"indicators"`
		} `json:"result"`
		Error *struct {
			Code        string `json:"code"`
			Description string `json:"description"`
		} `json:"error"`
	} `json:"chart"`
}

// FetchBars calls Yahoo's v8 chart API for ticker and returns real close
// bars, retrying transient failures the same way cmd/market-data-watcher's
// own fetchYahooOHLCV does (Yahoo's unofficial API returns 403 for what's
// really rate limiting as often as it returns 429).
//
// interval is "1m" for the three intraday offsets (t0/t15m/t1h) or "1d" for
// the three daily-close offsets (eod/t1d/t3d) -- callers pick based on
// Offset.IsIntraday(), never mix the two, since Yahoo's own 1m history only
// covers roughly the trailing 7-8 days.
func FetchBars(ctx context.Context, client *http.Client, ticker, interval, rangeStr string) ([]Bar, error) {
	sym := yahooSymbol(ticker)
	url := fmt.Sprintf("%s/%s?interval=%s&range=%s&includePrePost=true", yahooBaseURL, sym, interval, rangeStr)

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
				return out, err
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				return out, &httpretry.StatusError{StatusCode: resp.StatusCode, URL: url}
			}
			if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
				return out, fmt.Errorf("decode json: %w", err)
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
	if len(result.Indicators.Quote) == 0 {
		return nil, fmt.Errorf("no quote data for %s", sym)
	}
	quote := result.Indicators.Quote[0]

	bars := make([]Bar, 0, len(result.Timestamp))
	for i, ts := range result.Timestamp {
		if i >= len(quote.Close) || quote.Close[i] == nil {
			continue
		}
		bars = append(bars, Bar{Time: time.Unix(ts, 0).UTC(), Close: *quote.Close[i]})
	}
	return bars, nil
}

// QuoteAt fetches real bars for ticker at the right resolution for offset
// and returns the price as-of target (the nearest bar at or before it).
// ok=false means no usable bar exists yet (e.g. target is still in the
// future, or Yahoo's real history window doesn't reach back far enough) --
// a real, honest "not ready" signal, not a zero-value price.
func QuoteAt(ctx context.Context, client *http.Client, ticker string, offset Offset, target time.Time) (Bar, bool, error) {
	interval, rangeStr := "1d", "1mo"
	if offset.IsIntraday() {
		interval, rangeStr = "1m", "5d"
	}
	bars, err := FetchBars(ctx, client, ticker, interval, rangeStr)
	if err != nil {
		return Bar{}, false, err
	}
	bar, ok := NearestBarAtOrBefore(bars, target)
	return bar, ok, nil
}
