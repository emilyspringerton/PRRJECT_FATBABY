// Package bonddata fetches bond/treasury yield timeseries from FRED (the
// St. Louis Fed's Federal Reserve Economic Data service) -- a free,
// no-API-key-required public data source, same "no new vendor, no cost"
// shape as internal/movers (Yahoo) and internal/marketcal (computed, not a
// vendor at all). CSV export endpoint, no auth, no rate-limit headers
// observed in practice; still routed through httpretry for the same
// resiliency every other external collector in this repo uses.
package bonddata

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/example/prrject-fatbaby/internal/httpretry"
)

// fredBaseURL is a var, not a const, so tests can point it at a test server.
var fredBaseURL = "https://fred.stlouisfed.org/graph/fredgraph.csv"

// Series is one FRED series we track, with a human label for display.
type Series struct {
	ID    string // FRED series ID, e.g. "DGS10"
	Label string // e.g. "10-Year Treasury"
}

// TrackedSeries is the default set of series this package ingests --
// treasury yields spanning the curve plus one credit-risk indicator.
// Deliberately small at launch; add more once there's a real reason to
// (matches the phased-not-everything-at-once shape of every other
// auto-generated-articles phase).
var TrackedSeries = []Series{
	{ID: "DGS2", Label: "2-Year Treasury"},
	{ID: "DGS10", Label: "10-Year Treasury"},
	{ID: "DGS30", Label: "30-Year Treasury"},
	{ID: "BAMLH0A0HYM2", Label: "High Yield Corporate Spread"},
}

// Observation is one (date, value) point for one series.
type Observation struct {
	SeriesID string    `json:"series_id"`
	Label    string    `json:"label"`
	Date     time.Time `json:"date"`
	Value    float64   `json:"value"` // percent (yield or spread), FRED's native unit for these series
}

// FetchSeries downloads a FRED series' full CSV history and returns every
// parsed observation, oldest first. FRED marks missing days (holidays,
// data lag) with "." -- those rows are skipped, not returned as zero.
func FetchSeries(ctx context.Context, client *http.Client, series Series) ([]Observation, error) {
	url := fredBaseURL + "?id=" + series.ID

	obs, err := httpretry.Do(ctx, httpretry.Options{MaxRetries: 3, BackoffBase: 500 * time.Millisecond, BackoffCap: 8 * time.Second},
		func(ctx context.Context, attempt int) ([]Observation, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				return nil, err
			}
			req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; EinhornIndustrialBot/1.0)")

			resp, err := client.Do(req)
			if err != nil {
				return nil, err
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				return nil, &httpretry.StatusError{StatusCode: resp.StatusCode, URL: url}
			}
			return parseFredCSV(resp.Body, series)
		})
	if err != nil {
		return nil, fmt.Errorf("fetch series %s: %w", series.ID, err)
	}
	return obs, nil
}

// FetchLatest returns only the most recent observation for a series.
func FetchLatest(ctx context.Context, client *http.Client, series Series) (Observation, error) {
	all, err := FetchSeries(ctx, client, series)
	if err != nil {
		return Observation{}, err
	}
	if len(all) == 0 {
		return Observation{}, fmt.Errorf("series %s: no observations returned", series.ID)
	}
	return all[len(all)-1], nil
}

func parseFredCSV(r io.Reader, series Series) ([]Observation, error) {
	scanner := bufio.NewScanner(r)
	var out []Observation
	first := true
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if first {
			// Header row: "observation_date,DGS10" -- skip.
			first = false
			continue
		}
		parts := strings.SplitN(line, ",", 2)
		if len(parts) != 2 {
			continue
		}
		dateStr, valStr := parts[0], strings.TrimSpace(parts[1])
		if valStr == "." || valStr == "" {
			continue // FRED's missing-observation marker
		}
		date, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			continue
		}
		val, err := strconv.ParseFloat(valStr, 64)
		if err != nil {
			continue
		}
		out = append(out, Observation{SeriesID: series.ID, Label: series.Label, Date: date, Value: val})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
