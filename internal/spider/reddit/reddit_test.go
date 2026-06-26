package reddit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/example/prrject-fatbaby/internal/streamlog"
)

func makeListing(posts []map[string]any) []byte {
	children := make([]map[string]any, len(posts))
	for i, p := range posts {
		children[i] = map[string]any{"kind": "t3", "data": p}
	}
	listing := map[string]any{
		"kind": "Listing",
		"data": map[string]any{
			"children": children,
		},
	}
	b, _ := json.Marshal(listing)
	return b
}

func TestFetchSubreddit(t *testing.T) {
	now := time.Now().UTC()
	body := makeListing([]map[string]any{
		{
			"id":           "abc123",
			"subreddit":    "investing",
			"title":        "Stock market update",
			"selftext":     "Some body text",
			"url":          "https://example.com/article",
			"score":        150,
			"num_comments": 42,
			"created_utc":  float64(now.Unix()),
			"permalink":    "/r/investing/comments/abc123/stock_market_update/",
		},
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("expected User-Agent header")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer srv.Close()

	c := New(streamlog.Discard())
	c.RateLimit = time.Millisecond

	// Patch apiBase for test.
	oldBase := apiBase
	_ = oldBase // keep reference
	// Fetch using direct URL test.
	_ = c // verified via integration-style test using mock server

	// Test the full round-trip with a manual URL override via httptest.
	// We rebuild a minimal client to hit the test server.
	c2 := &Client{
		Log:       streamlog.Discard(),
		UserAgent: "test/1.0",
		RateLimit: time.Millisecond,
		client: &http.Client{
			Transport: &redirectTransport{base: srv.URL},
			Timeout:   5 * time.Second,
		},
	}

	posts, err := c2.FetchSubreddit(context.Background(), "investing", 10)
	if err != nil {
		t.Fatalf("FetchSubreddit error: %v", err)
	}
	if len(posts) != 1 {
		t.Fatalf("expected 1 post, got %d", len(posts))
	}
	p := posts[0]
	if p.ID != "abc123" {
		t.Errorf("expected id abc123, got %q", p.ID)
	}
	if p.Title != "Stock market update" {
		t.Errorf("unexpected title: %q", p.Title)
	}
	if p.Score != 150 {
		t.Errorf("expected score 150, got %d", p.Score)
	}
	if p.Permalink != "https://www.reddit.com/r/investing/comments/abc123/stock_market_update/" {
		t.Errorf("unexpected permalink: %q", p.Permalink)
	}
}

func TestFetchSubredditRateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := &Client{
		Log:       streamlog.Discard(),
		UserAgent: "test/1.0",
		RateLimit: time.Millisecond,
		client: &http.Client{
			Transport: &redirectTransport{base: srv.URL},
			Timeout:   5 * time.Second,
		},
	}
	_, err := c.FetchSubreddit(context.Background(), "investing", 5)
	if err == nil {
		t.Error("expected error on 429")
	}
}

func TestExternalLinks(t *testing.T) {
	posts := []Post{
		{LinkURL: "https://example.com/article"},
		{LinkURL: "https://www.reddit.com/r/investing/comments/abc/title/"},
		{LinkURL: "https://reuters.com/story"},
		{LinkURL: ""},
		{LinkURL: "https://example.com/article"}, // duplicate
	}
	links := ExternalLinks(posts)
	if len(links) != 2 {
		t.Errorf("expected 2 unique external links, got %d: %v", len(links), links)
	}
}

func TestStreamContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := New(streamlog.Discard())
	c.RateLimit = time.Millisecond
	ch := c.Stream(ctx, []string{"investing"}, time.Time{})

	var count int
	for range ch {
		count++
	}
	// With cancelled context the stream should not block or panic.
	_ = count
}

// redirectTransport redirects all requests to a test server base URL.
type redirectTransport struct {
	base string
	rt   http.RoundTripper
}

func (t *redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req2 := req.Clone(req.Context())
	req2.URL.Scheme = "http"
	req2.URL.Host = t.base[7:] // strip "http://"
	if t.rt != nil {
		return t.rt.RoundTrip(req2)
	}
	return http.DefaultTransport.RoundTrip(req2)
}
