// Package prspam detects the securities-litigation-solicitation genre of
// PRNewswire press release (SueWallSt, Pomerantz, Levi & Korsinsky, Robbins
// Geller/LLP, Wolf Haldenstein, Hagens Berman, The Gross Law Firm, DJS Law
// Group, and others not worth enumerating individually) -- a law firm
// soliciting plaintiffs against a company whose ticker just happens to
// appear in the release, e.g. "PNR SHAREHOLDER INVESTIGATION: SueWallSt
// Notifies Investors of Potential Securities Claims Involving Pentair plc".
//
// S170-07 (EMILY/BACKLOG.md): found while investigating guidance-watcher's
// "issuer=PNR SHAREHOLDER INV" bug that this genre isn't a rare edge case --
// it made up the overwhelming majority of guidance-watcher's live output
// (58 of 109 records) and, checked afterward, a similarly large share of
// dividend-watcher's (14 of 20). Both watchers' own trigger-word classifiers
// (guidance: "guidance|outlook|expects..."; dividend: "dividend|
// distribution" + raise/cut/special) have no way to distinguish a real
// corporate announcement from a real EPS or dividend figure quoted *inside*
// litigation-alert copy, attributed to a company that never issued it --
// this package exists so every prwatch-body consumer shares one filter
// instead of each reinventing (and independently under-covering) it.
//
// Also covers StripRelatedArticles (S170-231): a second, unrelated class of
// page cruft -- PRNewswire's "Also from this source" related-articles
// widget, which can tease a DIFFERENT real press release from the same
// company and fool a trigger-word classifier just as easily as litigation
// spam does.
package prspam

import (
	"regexp"
	"strings"
)

// reHeadlineTimePrefix strips a leading "HH:MM ET" scrape artifact -- and the
// blank/tab-indented lines PRNewswire's own listing markup wraps around it --
// that prwatch's discovery scraper (prwatch/client.go's h3Re) captures
// alongside the real title text. Confirmed live, S170-07: essentially every
// ev.Headline carries this, e.g. "13:46 ET\n\t\t\t\n\t\t\t\n\t\t\tPNR
// SHAREHOLDER INVESTIGATION: ...". Left unfixed, it garbles any downstream
// issuer-name extraction that keys off the start of the headline. Fixing
// this at the scrape source (prwatch/client.go) would touch every consumer
// of ev.Headline at once -- a bigger, riskier change than this package's own
// scope, so it's stripped defensively here instead.
var reHeadlineTimePrefix = regexp.MustCompile(`(?s)^\s*\d{1,2}:\d{2}\s*ET\s*`)

// reLitigationAlertHeadline is matched on phrase, not law-firm name, since
// the roster of firms running this playbook is long and any name list would
// immediately go stale; these phrases are near-universal boilerplate across
// the entire genre regardless of which firm wrote it (also covers the
// closely-related data-breach-class-action genre, e.g. Edelson Lechtzin's
// "Data Breach" alerts, and generic M&A-fairness solicitations like
// "Investigating Whether X Are Obtaining Fair Deals for their Shareholders"
// -- same solicit-plaintiffs playbook, different pretext). Measured against
// the full var/prwatch headline corpus (9625 unique headlines) before
// shipping: flags ~12%, with a manual review of both the flagged and
// unflagged sides finding no genuine company press release caught and no
// more than a stray, low-volume long tail of spam phrasing left uncaught --
// good enough to stop fabricating financial data, not a claim of exhaustive
// coverage.
var reLitigationAlertHeadline = regexp.MustCompile(`(?i)\b(shareholder alert|shareholder investigation|investor alert|` +
	`investor investigation|class action|securities claims|securities fraud|securities lawsuit|` +
	`securities law violations|notifies investors|reminds (?:shareholders|investors)|deadline alert|` +
	`lead plaintiff deadline|encourages investors|opportunity to lead|fair deals for|fiduciary duties|` +
	`data breach|law firm|lost money|investigat\w*)\b`)

// IsLitigationAlertHeadline reports whether title reads as a securities-
// litigation solicitation rather than a real company press release. Apply
// this before running a trigger-word classifier over the release body, not
// after -- letting one of these through can fabricate a structured financial
// event from a real dollar figure quoted out of context inside the
// litigation copy.
func IsLitigationAlertHeadline(title string) bool {
	return reLitigationAlertHeadline.MatchString(title)
}

// StripTimePrefix removes the leading "HH:MM ET" scrape artifact described
// on reHeadlineTimePrefix, so downstream issuer-name extraction that keys
// off the start of the headline isn't contaminated by it.
func StripTimePrefix(title string) string {
	return reHeadlineTimePrefix.ReplaceAllString(title, "")
}

// relatedArticlesMarker is PRNewswire's own "Also from this source" related-
// articles recommendation widget header. It sits inside the same page (often
// inside the release-body boundary extract_clean.go's own
// extractPRNewswireArticleBody looks for), teasing OTHER, unrelated press
// releases from the same company -- e.g. a "Target Announces Voting Results
// from 2026 Annual Meeting of Shareholders" release's own page embeds "Also
// from this source Target Corporation Increases Quarterly Dividend by 1.8
// Percent" a few paragraphs down. A trigger-word classifier over the full
// body text has no way to know that snippet describes a DIFFERENT press
// release, not the one it's actually classifying -- confirmed live, S170-231
// (EMILY/BACKLOG.md): this exact widget produced a fabricated dividend
// "raise" signal attributed to a routine shareholder-meeting-voting-results
// release that never announced a dividend change at all. Extremely common
// (2358 occurrences across the full var/prwatch-body corpus at the time this
// was found) -- not a rare edge case, though most occurrences are harmless
// (the teased related article doesn't happen to trip a trigger-word
// classifier). Same "strip known page cruft before classifying" philosophy
// as extractPRNewswireArticleBody's own nav-chrome fix, just for a different
// piece of surrounding-page noise, and applied at read-time rather than
// fetch-time since these watchers only ever read prwatch-body's already-
// stored bodies, never re-fetch.
const relatedArticlesMarker = "Also from this source"

// StripRelatedArticles truncates body at PRNewswire's "Also from this
// source" widget marker (see relatedArticlesMarker's own doc comment),
// discarding everything from that point on. Returns body unchanged if the
// marker isn't present -- same fail-open-to-the-original philosophy
// extractPRNewswireArticleBody already uses, never breaks classification
// outright if the marker moves or isn't there.
func StripRelatedArticles(body string) string {
	if idx := strings.Index(body, relatedArticlesMarker); idx >= 0 {
		return body[:idx]
	}
	return body
}
