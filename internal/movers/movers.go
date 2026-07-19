// Package movers fetches US market-wide gainers/losers from Yahoo Finance's
// free, unofficial predefined-screener endpoint. Same vendor/API family
// already integrated by cmd/market-data-watcher -- no new credential, no new
// cost. See PRRJECT_FATBABY/docs/northstar/auto-generated-articles.md for
// why this is market-wide rather than scoped to the tracked watchlist.
package movers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/example/prrject-fatbaby/internal/httpretry"
)

// screenerBaseURL is a var, not a const, so tests can point it at an
// httptest.Server (see movers_test.go's screenerBaseURLForTest helper).
var screenerBaseURL = "https://query1.finance.yahoo.com/v1/finance/screener/predefined/saved"

// Quote is one ticker's snapshot from the screener at fetch time.
type Quote struct {
	Symbol         string  `json:"symbol"`
	Name           string  `json:"name"`
	Exchange       string  `json:"exchange"`
	Price          float64 `json:"price"`
	Change         float64 `json:"change"`
	ChangePercent  float64 `json:"change_percent"`
	Volume         int64   `json:"volume"`
	PreviousClose  float64 `json:"previous_close"`
	MarketCap      int64   `json:"market_cap"`
}

// Snapshot is one fetch's worth of gainers and losers.
type Snapshot struct {
	FetchedAt time.Time `json:"fetched_at"`
	Gainers   []Quote   `json:"gainers"`
	Losers    []Quote   `json:"losers"`
}

// FetchSnapshot fetches both day_gainers and day_losers, up to count each.
func FetchSnapshot(ctx context.Context, client *http.Client, count int) (Snapshot, error) {
	gainers, err := FetchScreener(ctx, client, "day_gainers", count)
	if err != nil {
		return Snapshot{}, fmt.Errorf("fetch day_gainers: %w", err)
	}
	losers, err := FetchScreener(ctx, client, "day_losers", count)
	if err != nil {
		return Snapshot{}, fmt.Errorf("fetch day_losers: %w", err)
	}
	return Snapshot{FetchedAt: time.Now().UTC(), Gainers: gainers, Losers: losers}, nil
}

// FetchScreener fetches one predefined Yahoo screener (e.g. "day_gainers",
// "day_losers") and returns up to count quotes, retrying transient failures
// (429/403/5xx) with exponential backoff via httpretry.
func FetchScreener(ctx context.Context, client *http.Client, scrID string, count int) ([]Quote, error) {
	if count <= 0 {
		count = 25
	}
	url := fmt.Sprintf("%s?formatted=false&lang=en-US&region=US&scrIds=%s&count=%d", screenerBaseURL, scrID, count)

	quotes, err := httpretry.Do(ctx, httpretry.Options{MaxRetries: 3, BackoffBase: 500 * time.Millisecond, BackoffCap: 8 * time.Second},
		func(ctx context.Context, attempt int) ([]Quote, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				return nil, err
			}
			// Yahoo's unofficial endpoints reject requests with no User-Agent.
			req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; EinhornIndustrialBot/1.0)")

			resp, err := client.Do(req)
			if err != nil {
				return nil, err
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				return nil, &httpretry.StatusError{StatusCode: resp.StatusCode, URL: url}
			}

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				return nil, err
			}
			return parseScreenerResponse(body)
		})
	if err != nil {
		return nil, err
	}
	return quotes, nil
}

type screenerResponse struct {
	Finance struct {
		Result []struct {
			Quotes []screenerQuote `json:"quotes"`
		} `json:"result"`
		Error *struct {
			Code        string `json:"code"`
			Description string `json:"description"`
		} `json:"error"`
	} `json:"finance"`
}

type screenerQuote struct {
	Symbol                  string  `json:"symbol"`
	ShortName               string  `json:"shortName"`
	LongName                string  `json:"longName"`
	FullExchangeName        string  `json:"fullExchangeName"`
	RegularMarketPrice      float64 `json:"regularMarketPrice"`
	RegularMarketChange     float64 `json:"regularMarketChange"`
	RegularMarketChangePct  float64 `json:"regularMarketChangePercent"`
	RegularMarketVolume     int64   `json:"regularMarketVolume"`
	RegularMarketPrevClose  float64 `json:"regularMarketPreviousClose"`
	MarketCap               int64   `json:"marketCap"`
}

func parseScreenerResponse(body []byte) ([]Quote, error) {
	var resp screenerResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal screener response: %w", err)
	}
	if resp.Finance.Error != nil {
		return nil, fmt.Errorf("yahoo screener error %s: %s", resp.Finance.Error.Code, resp.Finance.Error.Description)
	}
	if len(resp.Finance.Result) == 0 {
		return nil, nil
	}
	sq := resp.Finance.Result[0].Quotes
	out := make([]Quote, 0, len(sq))
	for _, q := range sq {
		name := q.LongName
		if name == "" {
			name = q.ShortName
		}
		if name == "" {
			name = q.Symbol
		}
		out = append(out, Quote{
			Symbol:        strings.ToUpper(strings.TrimSpace(q.Symbol)),
			Name:          name,
			Exchange:      q.FullExchangeName,
			Price:         q.RegularMarketPrice,
			Change:        q.RegularMarketChange,
			ChangePercent: q.RegularMarketChangePct,
			Volume:        q.RegularMarketVolume,
			PreviousClose: q.RegularMarketPrevClose,
			MarketCap:     q.MarketCap,
		})
	}
	return out, nil
}
