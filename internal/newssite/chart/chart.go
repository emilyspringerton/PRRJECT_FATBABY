// Package chart renders recent equity prices as a minimal SVG sparkline.
// No external dependencies, no third-party chart embed — the SVG is drawn
// from EINHORN_INDUSTRIAL's own ingested OHLCV data (cmd/market-data-watcher
// -> internal/newssite/marketdata), not fetched live from Yahoo Finance at
// render time. Fetch/parseYahooResponse below remain only as the one-shot
// backfill path market-data-watcher itself uses; this package's own
// SVGCached no longer calls Yahoo directly (see the marketdata-source
// variant below) — kept for reference/tests, not the production render path.
package chart

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"
)

// PricePoint is one daily close price.
type PricePoint struct {
	Time  time.Time
	Close float64
}

// Fetch returns up to 3 months of daily closing prices for the given ticker
// from Yahoo Finance's unofficial v8 API. Returns nil, nil when the ticker
// has no price data (e.g. ETF instruments not listed on YF).
func Fetch(ticker string) ([]PricePoint, error) {
	url := fmt.Sprintf(
		"https://query1.finance.yahoo.com/v8/finance/chart/%s?interval=1d&range=3mo",
		ticker,
	)
	client := &http.Client{Timeout: 8 * time.Second}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 fatbaby-newssite/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", ticker, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("yahoo finance %d for %s", resp.StatusCode, ticker)
	}
	return parseYahooResponse(resp.Body)
}

type yahooChart struct {
	Chart struct {
		Result []struct {
			Timestamps []int64 `json:"timestamp"`
			Indicators struct {
				Quote []struct {
					Close []float64 `json:"close"`
				} `json:"quote"`
			} `json:"indicators"`
		} `json:"result"`
		Error *struct {
			Code        string `json:"code"`
			Description string `json:"description"`
		} `json:"error"`
	} `json:"chart"`
}

func parseYahooResponse(r io.Reader) ([]PricePoint, error) {
	var yc yahooChart
	if err := json.NewDecoder(r).Decode(&yc); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if yc.Chart.Error != nil {
		return nil, fmt.Errorf("yahoo error %s: %s", yc.Chart.Error.Code, yc.Chart.Error.Description)
	}
	if len(yc.Chart.Result) == 0 {
		return nil, nil
	}
	res := yc.Chart.Result[0]
	if len(res.Indicators.Quote) == 0 {
		return nil, nil
	}
	closes := res.Indicators.Quote[0].Close
	ts := res.Timestamps
	n := len(ts)
	if len(closes) < n {
		n = len(closes)
	}
	pts := make([]PricePoint, 0, n)
	for i := 0; i < n; i++ {
		if math.IsNaN(closes[i]) || closes[i] == 0 {
			continue
		}
		pts = append(pts, PricePoint{
			Time:  time.Unix(ts[i], 0).UTC(),
			Close: closes[i],
		})
	}
	return pts, nil
}

// RenderSVG returns an SVG sparkline string for the given price series.
// Width=400 Height=60. Returns an empty string if pts is empty.
func RenderSVG(pts []PricePoint) string {
	if len(pts) < 2 {
		return ""
	}

	const (
		svgW   = 400.0
		svgH   = 60.0
		padX   = 4.0
		padTop = 6.0
		padBot = 6.0
	)

	minP, maxP := pts[0].Close, pts[0].Close
	for _, p := range pts {
		if p.Close < minP {
			minP = p.Close
		}
		if p.Close > maxP {
			maxP = p.Close
		}
	}
	priceRange := maxP - minP
	if priceRange == 0 {
		priceRange = 1
	}

	scaleY := func(price float64) float64 {
		return padTop + (svgH-padTop-padBot)*(1-(price-minP)/priceRange)
	}
	scaleX := func(i int) float64 {
		return padX + float64(i)/float64(len(pts)-1)*(svgW-2*padX)
	}

	up := pts[len(pts)-1].Close >= pts[0].Close
	lineColor := "#16a34a"  // green
	fillColor := "#bbf7d0"  // green-100
	if !up {
		lineColor = "#dc2626" // red
		fillColor = "#fee2e2" // red-100
	}

	// Build polyline points
	var polyPts strings.Builder
	for i, p := range pts {
		if i > 0 {
			polyPts.WriteByte(' ')
		}
		fmt.Fprintf(&polyPts, "%.1f,%.1f", scaleX(i), scaleY(p.Close))
	}

	// Build fill polygon (close back along bottom edge)
	firstX := scaleX(0)
	lastX := scaleX(len(pts) - 1)
	var fillPts strings.Builder
	fillPts.WriteString(polyPts.String())
	fmt.Fprintf(&fillPts, " %.1f,%.1f %.1f,%.1f", lastX, svgH-padBot, firstX, svgH-padBot)

	// Price labels
	openStr := fmt.Sprintf("$%.2f", pts[0].Close)
	closeStr := fmt.Sprintf("$%.2f", pts[len(pts)-1].Close)

	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 400 60" width="400" height="60" style="display:block;max-width:100%%">
  <polygon points="%s" fill="%s" fill-opacity="0.35"/>
  <polyline points="%s" fill="none" stroke="%s" stroke-width="1.5" stroke-linejoin="round"/>
  <text x="%.1f" y="%.1f" font-family="system-ui,sans-serif" font-size="9" fill="#6b7280">%s</text>
  <text x="%.1f" y="%.1f" font-family="system-ui,sans-serif" font-size="9" fill="%s" text-anchor="end">%s</text>
</svg>`,
		fillPts.String(),
		fillColor,
		polyPts.String(),
		lineColor,
		firstX, svgH-1.0, openStr,
		lastX, svgH-1.0, lineColor, closeStr,
	)
}

// --- In-memory render cache (SVG string building is cheap but not free;
// the underlying data only changes ~once/day via market-data-watcher, so a
// short cache avoids redundant work on hot ticker pages without ever
// risking stale-vs-source drift for long) ---

type cacheEntry struct {
	svg        string
	renderedAt time.Time
}

var (
	cacheMu sync.Mutex
	cache   = map[string]*cacheEntry{}
)

const cacheTTL = 10 * time.Minute

// SVGCachedFromPoints returns an SVG sparkline for ticker built from pts
// (already-converted, EINHORN_INDUSTRIAL's own ingested data — never a live
// third-party fetch), cached for cacheTTL. Caller (newssite handler) owns
// pulling pts from internal/newssite/marketdata.Store and converting
// Bar -> PricePoint; this package stays free of any dependency on how the
// data is stored.
func SVGCachedFromPoints(ticker string, pts []PricePoint) string {
	cacheMu.Lock()
	entry, ok := cache[ticker]
	if ok && time.Since(entry.renderedAt) < cacheTTL {
		cacheMu.Unlock()
		return entry.svg
	}
	cacheMu.Unlock()

	svg := RenderSVG(pts)

	cacheMu.Lock()
	cache[ticker] = &cacheEntry{svg: svg, renderedAt: time.Now()}
	cacheMu.Unlock()
	return svg
}
