// Package tickerlink is the one shared implementation of EINHORN_INDUSTRIAL's
// ticker-reference editorial standard: "Company Name (EXCHANGE:TICKER)",
// with the ticker itself hyperlinked to our own ticker page.
//
// Every content generator (movers-watcher today; anything else Gauntlet
// eventually manages) should format ticker references through this package
// rather than hand-rolling the pattern — see EMILY/BACKLOG.md SECTION 167.
package tickerlink

import (
	"fmt"
	"html"
	"html/template"
	"net/url"
	"strings"
)

// FormatRef returns "Company Name (EXCHANGE:TICKER)" as trusted HTML, with
// TICKER linked to baseURL + "/ticker/{TICKER}". baseURL must be the
// canonical public origin (e.g. "https://news.okemily.com") -- an absolute
// URL, not a relative path, so the link still resolves when this content is
// copied, syndicated, or redistributed elsewhere. All inputs are
// HTML-escaped; ticker is additionally URL-escaped for the href.
//
// exchange may be empty, in which case the parenthetical is just the ticker
// (no leading "EXCHANGE:") -- some data sources (including Yahoo's
// screener) report tickers without a reliable exchange code.
func FormatRef(baseURL, companyName, exchange, ticker string) template.HTML {
	ticker = strings.ToUpper(strings.TrimSpace(ticker))
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")

	href := baseURL + "/ticker/" + url.PathEscape(ticker)
	link := fmt.Sprintf(`<a href="%s">%s</a>`, html.EscapeString(href), html.EscapeString(ticker))

	prefix := ""
	if strings.TrimSpace(exchange) != "" {
		prefix = html.EscapeString(strings.ToUpper(strings.TrimSpace(exchange))) + ":"
	}

	return template.HTML(fmt.Sprintf("%s (%s%s)", html.EscapeString(companyName), prefix, link)) //nolint:gosec // all interpolated values are individually escaped above
}

// PlainRef returns the same "Company Name (EXCHANGE:TICKER)" text with no
// markup, for plain-text bodies/previews/RSS descriptions that shouldn't
// carry HTML — the ticker is included as plain text, not a link, in that
// context. FormatRef is the version with a real hyperlink.
func PlainRef(companyName, exchange, ticker string) string {
	ticker = strings.ToUpper(strings.TrimSpace(ticker))
	prefix := ""
	if strings.TrimSpace(exchange) != "" {
		prefix = strings.ToUpper(strings.TrimSpace(exchange)) + ":"
	}
	return fmt.Sprintf("%s (%s%s)", companyName, prefix, ticker)
}
