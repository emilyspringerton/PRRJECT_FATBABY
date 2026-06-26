// Package reddit provides a log-streaming Reddit API client for the Emily
// research engine. Uses Reddit's public JSON API (no auth required) with a
// 2-second rate limit per Reddit API guidelines.
//
// Subreddit data format: https://reddit.com/r/{sub}/new.json
package reddit

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/example/prrject-fatbaby/internal/streamlog"
)

const (
	apiBase       = "https://www.reddit.com"
	defaultUA     = "EinhornIndustrialBot/1.0 (emily research; emilyspringerton@gmail.com)"
	defaultLimit  = 25
	defaultRate   = 2 * time.Second // Reddit API ToS
)

// FinancialSubs are the default subreddits for financial research.
var FinancialSubs = []string{
	"investing", "StockMarket", "wallstreetbets",
	"options", "ValueInvesting", "SecurityAnalysis",
}

// SupplyChainSubs are the default subreddits for supply chain research.
var SupplyChainSubs = []string{
	"supplychain", "logistics", "manufacturing",
	"procurement", "shipping",
}

// AITechSubs are the default subreddits for AI/tech research.
var AITechSubs = []string{
	"MachineLearning", "artificial", "ChatGPT",
	"LocalLLaMA", "singularity",
}

// AllResearchSubs is the union of all research domain subreddits.
var AllResearchSubs = append(append(FinancialSubs, SupplyChainSubs...), AITechSubs...)

// Post is one Reddit post.
type Post struct {
	ID           string
	Subreddit    string
	Title        string
	Body         string // selftext; empty for link posts
	LinkURL      string // external link URL (may equal permalink for text posts)
	Score        int
	CommentCount int
	CreatedAt    time.Time
	Permalink    string
}

// redditListing is the JSON response envelope from Reddit's API.
type redditListing struct {
	Data struct {
		Children []struct {
			Data struct {
				ID          string  `json:"id"`
				Subreddit   string  `json:"subreddit"`
				Title       string  `json:"title"`
				Selftext    string  `json:"selftext"`
				URL         string  `json:"url"`
				Score       int     `json:"score"`
				NumComments int     `json:"num_comments"`
				CreatedUTC  float64 `json:"created_utc"`
				Permalink   string  `json:"permalink"`
			} `json:"data"`
		} `json:"children"`
	} `json:"data"`
}

// Client fetches posts from Reddit with rate limiting and stream logging.
type Client struct {
	Log       *streamlog.Logger
	UserAgent string
	RateLimit time.Duration
	client    *http.Client
	lastFetch time.Time
}

// New returns a Client with sensible defaults. Pass streamlog.Discard() in tests.
func New(log *streamlog.Logger) *Client {
	return &Client{
		Log:       log,
		UserAgent: defaultUA,
		RateLimit: defaultRate,
		client:    &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *Client) ua() string {
	if c.UserAgent != "" {
		return c.UserAgent
	}
	return defaultUA
}

func (c *Client) rate() time.Duration {
	if c.RateLimit > 0 {
		return c.RateLimit
	}
	return defaultRate
}

func (c *Client) log() *streamlog.Logger {
	if c.Log != nil {
		return c.Log
	}
	return streamlog.Discard()
}

// wait enforces the rate limit before each API call.
func (c *Client) wait(ctx context.Context) error {
	gap := c.rate() - time.Since(c.lastFetch)
	if gap <= 0 {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(gap):
		return nil
	}
}

// FetchSubreddit retrieves up to limit posts from a subreddit sorted by /new.
func (c *Client) FetchSubreddit(ctx context.Context, sub string, limit int) ([]Post, error) {
	if limit <= 0 {
		limit = defaultLimit
	}
	if err := c.wait(ctx); err != nil {
		return nil, err
	}
	c.lastFetch = time.Now()

	url := fmt.Sprintf("%s/r/%s/new.json?limit=%d&raw_json=1", apiBase, sub, limit)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", c.ua())

	resp, err := c.client.Do(req)
	if err != nil {
		c.log().Error("reddit", "fetch failed: r/"+sub, err)
		return nil, fmt.Errorf("fetch r/%s: %w", sub, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 429 {
		c.log().Warn("reddit", "rate limited", map[string]any{"sub": sub})
		return nil, fmt.Errorf("rate limited by Reddit (r/%s)", sub)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("r/%s: HTTP %d", sub, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read r/%s: %w", sub, err)
	}
	c.log().Fetch("reddit", url, resp.StatusCode, int64(len(body)))

	var listing redditListing
	if err := json.Unmarshal(body, &listing); err != nil {
		return nil, fmt.Errorf("parse r/%s: %w", sub, err)
	}

	posts := make([]Post, 0, len(listing.Data.Children))
	for _, ch := range listing.Data.Children {
		d := ch.Data
		p := Post{
			ID:           d.ID,
			Subreddit:    d.Subreddit,
			Title:        d.Title,
			Body:         d.Selftext,
			LinkURL:      d.URL,
			Score:        d.Score,
			CommentCount: d.NumComments,
			CreatedAt:    time.Unix(int64(d.CreatedUTC), 0).UTC(),
			Permalink:    "https://www.reddit.com" + d.Permalink,
		}
		posts = append(posts, p)
	}
	c.log().Info("reddit", fmt.Sprintf("r/%s: got %d posts", sub, len(posts)))
	return posts, nil
}

// Stream fetches posts from multiple subreddits since a given time and sends
// them to the returned channel. The channel is closed when all subs are
// exhausted or ctx is cancelled.
func (c *Client) Stream(ctx context.Context, subs []string, since time.Time) <-chan Post {
	ch := make(chan Post, 64)
	go func() {
		defer close(ch)
		for _, sub := range subs {
			posts, err := c.FetchSubreddit(ctx, sub, defaultLimit)
			if err != nil {
				c.log().Warn("reddit", "skipping r/"+sub, map[string]any{"error": err.Error()})
				continue
			}
			for _, p := range posts {
				if p.CreatedAt.Before(since) {
					continue
				}
				select {
				case ch <- p:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return ch
}

// ExternalLinks returns the set of non-Reddit URLs linked from a slice of posts.
// This is the "spider out" step: follow external links from Reddit threads.
func ExternalLinks(posts []Post) []string {
	seen := make(map[string]bool)
	var links []string
	for _, p := range posts {
		u := p.LinkURL
		if u != "" &&
			!seen[u] &&
			len(u) > 8 &&
			u[:4] == "http" &&
			!isRedditURL(u) {
			seen[u] = true
			links = append(links, u)
		}
	}
	return links
}

func isRedditURL(u string) bool {
	return len(u) >= 18 && (u[8:18] == "reddit.com" || u[8:22] == "www.reddit.com")
}
