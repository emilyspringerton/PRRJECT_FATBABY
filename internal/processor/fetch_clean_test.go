package processor

import (
	"strings"
	"testing"
)

// TestExtractPRNewswireArticleBody_StripsNavChrome is a regression test for
// a real, live bug found 2026-07-19: dividend-watcher's classifier was
// false-positiving on law-firm "INVESTOR ALERT" press releases because
// CleanText was stripping tags from PRNewswire's entire page, including a
// site-navigation category menu that contains the word "Dividends" as one
// of ~80 unrelated topic filters. This fixture mirrors that real structure
// (trimmed from the actual page, not fabricated) -- nav chrome containing
// "Dividends" outside the article, real article text inside release-body
// that never mentions dividends at all.
func TestExtractPRNewswireArticleBody_StripsNavChrome(t *testing.T) {
	html := `<html><body>
<nav>Business & Money Auto & Transportation Banking & Financial Services
Bond & Stock Ratings Conference Call Announcements Contracts Cryptocurrency
Dividends Earnings Earnings Forecasts & Projections Financing Agreements
Insurance Investments Opinions Joint Ventures Mutual Funds</nav>
<div class="release-body container ">
  <div class="row"><div class="col-lg-10 col-lg-offset-1">
    <p>NEW YORK, July 16, 2026 /PRNewswire/ -- Pomerantz LLP is investigating
    claims on behalf of investors of Pentair plc.</p>
  </div></div>
</div>
<div class="row releaseList-section">
  <a href="/news-releases/other-unrelated-article">Related: Some Other Company Raises Dividend</a>
</div>
</body></html>`

	got := extractPRNewswireArticleBody(html)

	if strings.Contains(got, "Dividends") {
		t.Errorf("extracted body still contains the nav-menu 'Dividends' category — nav chrome not stripped:\n%s", got)
	}
	if strings.Contains(got, "Related: Some Other Company") {
		t.Errorf("extracted body leaked past the release-body boundary into the related-content section:\n%s", got)
	}
	if !strings.Contains(got, "Pomerantz LLP is investigating") {
		t.Errorf("extracted body is missing the real article text:\n%s", got)
	}
}

// TestExtractPRNewswireArticleBody_FailsOpenWhenMarkersMissing verifies the
// fallback: if PRNewswire's markup doesn't match (a redesign, a different
// page type), return the original page rather than an empty/broken string
// -- degrades to the old (noisy but working) behavior, never breaks body
// fetching outright.
func TestExtractPRNewswireArticleBody_FailsOpenWhenMarkersMissing(t *testing.T) {
	html := `<html><body><p>Some page with no release-body marker at all.</p></body></html>`
	got := extractPRNewswireArticleBody(html)
	if got != html {
		t.Errorf("expected fail-open passthrough when start marker is missing, got:\n%s", got)
	}
}

// TestExtractPRNewswireArticleBody_FailsOpenWhenEndMarkerMissing verifies
// the start-only fallback: if the end boundary is missing but the start
// marker is present, return everything from the start marker onward rather
// than nothing.
func TestExtractPRNewswireArticleBody_FailsOpenWhenEndMarkerMissing(t *testing.T) {
	html := `<html><body><nav>Dividends Earnings</nav><div class="release-body container "><p>Real article text with no end marker.</p></div></body></html>`
	got := extractPRNewswireArticleBody(html)
	if strings.Contains(got, "Dividends") {
		t.Errorf("expected nav chrome before the start marker to be stripped even without an end marker:\n%s", got)
	}
	if !strings.Contains(got, "Real article text") {
		t.Errorf("expected real article text to survive the start-only fallback:\n%s", got)
	}
}
