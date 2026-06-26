// Package spider fetches web pages with rate limiting and structured log streaming.
// Every fetch and extract operation emits an event to the provided streamlog.Logger
// so callers can monitor progress in real-time.
package spider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/example/prrject-fatbaby/internal/streamlog"
)

const defaultUserAgent = "EinhornIndustrialBot/1.0 (emily research engine; research@einhornindustrial.com)"
const defaultMaxBody = 2 << 20 // 2 MB

// Page is a fetched and extracted web page.
type Page struct {
	URL        string
	FetchedAt  time.Time
	StatusCode int
	Title      string
	BodyText   string // whitespace-collapsed visible text
	Links      []string
	ByteCount  int64
}

// Result wraps a Page or an error from a concurrent FetchMulti call.
type Result struct {
	Page *Page
	Err  error
}

// Spider fetches URLs with rate limiting. Zero-value is ready to use with defaults.
type Spider struct {
	Log         *streamlog.Logger
	RateLimit   time.Duration // min gap between requests; default 500ms
	UserAgent   string
	MaxBodySize int64
	client      *http.Client
	lastFetch   time.Time
}

func (s *Spider) log() *streamlog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return streamlog.Discard()
}

func (s *Spider) rateLimit() time.Duration {
	if s.RateLimit > 0 {
		return s.RateLimit
	}
	return 500 * time.Millisecond
}

func (s *Spider) maxBody() int64 {
	if s.MaxBodySize > 0 {
		return s.MaxBodySize
	}
	return defaultMaxBody
}

func (s *Spider) ua() string {
	if s.UserAgent != "" {
		return s.UserAgent
	}
	return defaultUserAgent
}

func (s *Spider) httpClient() *http.Client {
	if s.client == nil {
		s.client = &http.Client{Timeout: 15 * time.Second}
	}
	return s.client
}

// Fetch retrieves one URL, enforcing the rate limit and emitting log events.
func (s *Spider) Fetch(ctx context.Context, rawURL string) (*Page, error) {
	// Enforce rate limit.
	if gap := time.Since(s.lastFetch); gap < s.rateLimit() {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(s.rateLimit() - gap):
		}
	}
	s.lastFetch = time.Now()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		s.log().Error("spider", "build request failed", err)
		return nil, err
	}
	req.Header.Set("User-Agent", s.ua())

	resp, err := s.httpClient().Do(req)
	if err != nil {
		s.log().Error("spider", "fetch failed: "+rawURL, err)
		return nil, fmt.Errorf("fetch %s: %w", rawURL, err)
	}
	defer resp.Body.Close()

	limited := io.LimitReader(resp.Body, s.maxBody())
	raw, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read body %s: %w", rawURL, err)
	}

	p := &Page{
		URL:        rawURL,
		FetchedAt:  time.Now().UTC(),
		StatusCode: resp.StatusCode,
		ByteCount:  int64(len(raw)),
	}

	body := string(raw)
	p.Title = extractTitle(body)
	p.BodyText = collapseWhitespace(stripTags(body))
	p.Links = extractLinks(body, rawURL)

	s.log().Fetch("spider", rawURL, resp.StatusCode, p.ByteCount)
	s.log().Extract("spider", rawURL, len(p.Links))

	return p, nil
}

// FetchMulti fetches multiple URLs concurrently (one goroutine per URL) and
// streams Results to the returned channel. The channel is closed when all
// fetches complete or ctx is cancelled.
func (s *Spider) FetchMulti(ctx context.Context, urls []string) <-chan Result {
	ch := make(chan Result, len(urls))
	go func() {
		defer close(ch)
		// Sequential to respect rate limit within the same Spider instance.
		for _, u := range urls {
			p, err := s.Fetch(ctx, u)
			select {
			case ch <- Result{Page: p, Err: err}:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch
}

// stripTags removes HTML tags naively (good enough for text extraction).
func stripTags(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	in := false
	for i := 0; i < len(s); {
		r, sz := utf8.DecodeRuneInString(s[i:])
		i += sz
		switch {
		case r == '<':
			in = true
		case r == '>':
			in = false
			b.WriteByte(' ')
		case !in:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// collapseWhitespace replaces runs of whitespace with a single space.
func collapseWhitespace(s string) string {
	parts := strings.Fields(s)
	return strings.Join(parts, " ")
}

// extractTitle pulls text from the first <title> tag.
func extractTitle(s string) string {
	lower := strings.ToLower(s)
	start := strings.Index(lower, "<title>")
	if start < 0 {
		return ""
	}
	start += len("<title>")
	end := strings.Index(lower[start:], "</title>")
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(s[start : start+end])
}

// extractLinks pulls href values from <a> tags.
func extractLinks(body, baseURL string) []string {
	lower := strings.ToLower(body)
	var links []string
	pos := 0
	for {
		idx := strings.Index(lower[pos:], "href=\"")
		if idx < 0 {
			break
		}
		start := pos + idx + len("href=\"")
		end := strings.Index(lower[start:], "\"")
		if end < 0 {
			break
		}
		href := strings.TrimSpace(body[start : start+end])
		if href != "" && !strings.HasPrefix(href, "#") {
			if strings.HasPrefix(href, "http") {
				links = append(links, href)
			} else if strings.HasPrefix(href, "/") && baseURL != "" {
				// Resolve against host; strip any existing path from baseURL first.
				host := baseURL
				if i := strings.Index(baseURL[8:], "/"); i >= 0 {
					host = baseURL[:8+i]
				}
				links = append(links, host+href)
			}
		}
		pos = start + end + 1
	}
	return links
}
