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

		// Read up to 3× maxBytes so we can clean HTML and still hit maxBytes of clean text.
		raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes*3))
		resp.Body.Close()
		if err != nil {
			return "", fmt.Errorf("read primary document: %w", err)
		}
		body := string(raw)
		if strings.Contains(host, "prnewswire.com") {
			body = extractPRNewswireArticleBody(body)
		}
		cleaned := CleanText(body)
		// Truncate cleaned text to maxBytes of UTF-8 characters rather than failing.
		if int64(len(cleaned)) > maxBytes {
			cleaned = cleaned[:maxBytes]
			// Trim to last space to avoid splitting a word.
			if i := strings.LastIndex(cleaned, " "); i > int(maxBytes)*9/10 {
				cleaned = cleaned[:i]
			}
		}
		return cleaned, nil
	}
}

// prNewswireBodyStart/End bound the actual article content on a
// prnewswire.com release page. Found live (2026-07-19): CleanText was
// stripping tags from the *entire* page including PRNewswire's own site
// navigation -- a category menu of ~80 topic names (one of which is
// literally "Dividends") that isn't part of the article at all. That
// menu alone was enough to false-positive dividend-watcher's keyword
// classifier on unrelated law-firm "INVESTOR ALERT" class-action spam
// (confirmed: 10 of 13 records in var/dividends/dividends.ndjson were
// this exact false positive, not real dividend announcements).
var (
	prNewswireBodyStart = "release-body container"
	prNewswireBodyEnd   = "row releaseList-section"
)

// extractPRNewswireArticleBody isolates the real article HTML on a
// prnewswire.com page, before tag-stripping. Fails open to the full page
// (same as before this fix) if either boundary marker isn't found --
// PRNewswire changing their markup should degrade to the old (noisy but
// working) behavior, never break body fetching outright.
func extractPRNewswireArticleBody(html string) string {
	start := strings.Index(html, prNewswireBodyStart)
	if start < 0 {
		return html
	}
	end := strings.Index(html[start:], prNewswireBodyEnd)
	if end < 0 {
		return html[start:]
	}
	return html[start : start+end]
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
