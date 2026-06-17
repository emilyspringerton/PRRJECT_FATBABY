package processor

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

var (
	reScriptStyle    = regexp.MustCompile(`(?is)<(script|style)[^>]*>.*?</(script|style)>`)
	reXBRLTags       = regexp.MustCompile(`(?is)</?(ix:[^>\s]+|xbrli:[^>\s]+|link:[^>\s]+|dei:[^>\s]+|us-gaap:[^>\s]+)[^>]*>`)
	reAllTags        = regexp.MustCompile(`(?s)<[^>]+>`)
	reHTMLWhitespace = regexp.MustCompile(`\s+`)
)

// per-host rate limiting: at most one request per hostMinInterval per host.
var (
	hostRateMu   sync.Mutex
	hostLastReq  = make(map[string]time.Time)
)

const hostMinInterval = 150 * time.Millisecond // ~6 req/s per host

func throttleHost(ctx context.Context, host string) error {
	hostRateMu.Lock()
	last := hostLastReq[host]
	next := last.Add(hostMinInterval)
	now := time.Now()
	var wait time.Duration
	if now.Before(next) {
		wait = next.Sub(now)
	}
	hostLastReq[host] = now.Add(wait)
	hostRateMu.Unlock()
	if wait > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
	return nil
}

// retry429Delays are the wait times between successive 429 retries.
var retry429Delays = []time.Duration{30 * time.Second, 60 * time.Second, 300 * time.Second}

// IsValidDocURL returns true if the URL is non-empty and uses http or https.
func IsValidDocURL(docURL string) bool {
	return docURL != "" && (strings.HasPrefix(docURL, "http://") || strings.HasPrefix(docURL, "https://"))
}

func FetchAndCleanText(ctx context.Context, docURL, userAgent string, maxBytes int64) (string, error) {
	if maxBytes <= 0 {
		maxBytes = 4 << 20
	}
	if !IsValidDocURL(docURL) {
		return "", fmt.Errorf("fetch primary document: unsupported URL %q", docURL)
	}
	parsed, err := url.Parse(docURL)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	host := parsed.Host

	for attempt := 0; ; attempt++ {
		if err := throttleHost(ctx, host); err != nil {
			return "", err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, docURL, nil)
		if err != nil {
			return "", fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", fmt.Errorf("fetch primary document: %w", err)
		}

		if resp.StatusCode == http.StatusTooManyRequests && attempt < len(retry429Delays) {
			resp.Body.Close()
			delay := retry429Delays[attempt]
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(delay):
			}
			continue
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			resp.Body.Close()
			return "", fmt.Errorf("fetch primary document status=%d", resp.StatusCode)
		}

		raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
		resp.Body.Close()
		if err != nil {
			return "", fmt.Errorf("read primary document: %w", err)
		}
		if int64(len(raw)) > maxBytes {
			return "", fmt.Errorf("filing document too large: %d bytes > %d", len(raw), maxBytes)
		}
		return CleanText(string(raw)), nil
	}
}

func CleanText(raw string) string {
	withoutScript := reScriptStyle.ReplaceAllString(raw, " ")
	withoutXBRL := reXBRLTags.ReplaceAllString(withoutScript, " ")
	withoutTags := reAllTags.ReplaceAllString(withoutXBRL, " ")
	withoutEntities := htmlEntityDecode(withoutTags)
	return strings.TrimSpace(reHTMLWhitespace.ReplaceAllString(withoutEntities, " "))
}

var entityReplacer = strings.NewReplacer(
	"&nbsp;", " ",
	"&#160;", " ",
	"&amp;", "&",
	"&lt;", "<",
	"&gt;", ">",
	"&quot;", `"`,
	"&#39;", "'",
)

func htmlEntityDecode(s string) string { return entityReplacer.Replace(s) }
