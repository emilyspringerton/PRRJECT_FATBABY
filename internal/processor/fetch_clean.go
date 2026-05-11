package processor

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

var (
	reScriptStyle    = regexp.MustCompile(`(?is)<(script|style)[^>]*>.*?</(script|style)>`)
	reXBRLTags       = regexp.MustCompile(`(?is)</?(ix:[^>\s]+|xbrli:[^>\s]+|link:[^>\s]+|dei:[^>\s]+|us-gaap:[^>\s]+)[^>]*>`)
	reAllTags        = regexp.MustCompile(`(?s)<[^>]+>`)
	reHTMLWhitespace = regexp.MustCompile(`\s+`)
)

func FetchAndCleanText(ctx context.Context, docURL, userAgent string, maxBytes int64) (string, error) {
	if maxBytes <= 0 {
		maxBytes = 4 << 20
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
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("fetch primary document status=%d", resp.StatusCode)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return "", fmt.Errorf("read primary document: %w", err)
	}
	if int64(len(raw)) > maxBytes {
		return "", fmt.Errorf("filing document too large: %d bytes > %d", len(raw), maxBytes)
	}

	return CleanText(string(raw)), nil
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
