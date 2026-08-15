// Package fedwatch is S165-03 (Phase 3): Federal Reserve / FOMC data.
// Two pieces, per the backlog item's own scoping: (a) this file, an RSS
// poller over the Fed's public monetary-policy press feed, same "no key,
// no vendor" shape as internal/bonddata (FRED) and internal/movers
// (Yahoo); (b) calendar.go, a small fixed calendar of published FOMC
// meeting dates -- announced yearly by the Fed, not rule-computable like
// marketcal's NYSE holidays, so it's data, not a formula, same distinction
// marketcal.go itself draws between its two halves.
package fedwatch

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/example/prrject-fatbaby/internal/httpretry"
)

// feedURL is a var, not a const, so tests can point it at a test server --
// same convention internal/bonddata's fredBaseURL uses.
var feedURL = "https://www.federalreserve.gov/feeds/press_monetary.xml"

// rssFeed/rssItem mirror only the fields this package reads from the
// Fed's real RSS 2.0 feed (verified live 2026-08-15): title, link, guid,
// description, category, pubDate. encoding/xml, not regex -- unlike
// prwatch's own HTML scrape (PR Newswire has no feed), this source is
// already well-formed RSS, so parsing it as RSS is the correct tool, not
// a borrowed one.
type rssFeed struct {
	XMLName xml.Name   `xml:"rss"`
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Items []rssItem `xml:"item"`
}

type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	GUID        string `xml:"guid"`
	Description string `xml:"description"`
	Category    string `xml:"category"`
	PubDate     string `xml:"pubDate"`
}

// PressRelease is one discovered Fed monetary-policy press release.
type PressRelease struct {
	ID          string // the GUID, which the feed sets to the same value as Link -- verified live, not assumed
	Title       string
	URL         string
	Description string
	Category    string
	PublishedAt time.Time
}

// Client fetches the Fed's monetary-policy RSS feed.
type Client struct {
	hc  *http.Client
	url string
}

type ClientConfig struct {
	HTTPClient *http.Client
	FeedURL    string
}

func NewClient(cfg ClientConfig) *Client {
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 20 * time.Second}
	}
	if cfg.FeedURL == "" {
		cfg.FeedURL = feedURL
	}
	return &Client{hc: cfg.HTTPClient, url: cfg.FeedURL}
}

// Discover fetches and parses the feed, returning every item in feed
// order (newest first, matching the Fed's own publication order --
// verified live, not assumed).
func (c *Client) Discover(ctx context.Context) ([]PressRelease, error) {
	body, err := httpretry.Do(ctx, httpretry.Options{MaxRetries: 3, BackoffBase: 500 * time.Millisecond, BackoffCap: 8 * time.Second},
		func(ctx context.Context, attempt int) ([]byte, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
			if err != nil {
				return nil, err
			}
			req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; EinhornIndustrialBot/1.0)")
			req.Header.Set("Accept", "application/rss+xml, text/xml")

			resp, err := c.hc.Do(req)
			if err != nil {
				return nil, err
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return nil, &httpretry.StatusError{StatusCode: resp.StatusCode, URL: c.url}
			}
			return io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		})
	if err != nil {
		return nil, fmt.Errorf("fedwatch: fetch feed: %w", err)
	}

	var feed rssFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, fmt.Errorf("fedwatch: parse feed: %w", err)
	}

	out := make([]PressRelease, 0, len(feed.Channel.Items))
	for _, it := range feed.Channel.Items {
		id := strings.TrimSpace(it.GUID)
		if id == "" {
			id = strings.TrimSpace(it.Link)
		}
		if id == "" {
			continue // no stable identity for this item -- skip rather than dedupe on title alone
		}
		out = append(out, PressRelease{
			ID:          id,
			Title:       strings.TrimSpace(it.Title),
			URL:         strings.TrimSpace(it.Link),
			Description: strings.TrimSpace(it.Description),
			Category:    strings.TrimSpace(it.Category),
			PublishedAt: parsePubDate(it.PubDate),
		})
	}
	return out, nil
}

// parsePubDate parses RFC1123Z-shaped RSS pubDate values. The Fed's feed
// uses "Wed, 29 Jul 2026 18:00:00 GMT" (verified live) -- RFC1123 with a
// named zone, which Go's time.RFC1123 layout handles directly.
func parsePubDate(v string) time.Time {
	v = strings.TrimSpace(v)
	if v == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC1123, time.RFC1123Z} {
		if t, err := time.Parse(layout, v); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}
