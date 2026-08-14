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

// TestCleanText_StripsInlineXBRLHeaderContent is a regression test for a
// real, live bug found 2026-08-14 (founder: SEC filings display was "a
// garbled mess"). Confirmed against a real, live-fetched Costco 10-Q
// (cost-20260510.htm): the <ix:header> block was 58,806 characters out of
// an 897,626-character document (~6.5%), sitting at the very start, before
// any real visible content. reXBRLTags only ever stripped the XBRL tag
// WRAPPERS (<ix:...>, <xbrli:...>, etc.), never the raw context/unit/entity
// identifier TEXT they wrap -- so a customer.newssite reader hit that raw
// XBRL soup first, ahead of the real filing text. This fixture mirrors the
// real structure (tag names, nesting, attribute shape all real; content
// trimmed for a lean test), not fabricated from scratch.
func TestCleanText_StripsInlineXBRLHeaderContent(t *testing.T) {
	html := `<html><body><div style="display:none">
<ix:header><ix:hidden><ix:nonNumeric contextRef="c-1" name="dei:AmendmentFlag" format="ixt:fixed-false" id="f-24">FALSE</ix:nonNumeric><ix:nonNumeric contextRef="c-1" name="dei:EntityRegistrantName" id="f-27">COSTCO WHOLESALE CORP /NEW</ix:nonNumeric><ix:nonNumeric contextRef="c-1" name="dei:EntityCentralIndexKey" id="f-28">0000909832</ix:nonNumeric></ix:hidden><xbrli:context id="c-1"><xbrli:entity><xbrli:identifier scheme="http://www.sec.gov/CIK">0000909832</xbrli:identifier></xbrli:entity></xbrli:context><xbrli:unit id="usd"><xbrli:measure>iso4217:USD</xbrli:measure></xbrli:unit></ix:header>
</div>
<p>Costco Wholesale Corporation today reported net sales of $62.53 billion for the third quarter.</p>
</body></html>`

	got := CleanText(html)

	if strings.Contains(got, "AmendmentFlag") || strings.Contains(got, "EntityCentralIndexKey") || strings.Contains(got, "iso4217") {
		t.Errorf("cleaned text still contains ix:header XBRL identifier soup:\n%s", got)
	}
	if !strings.Contains(got, "reported net sales of $62.53 billion") {
		t.Errorf("cleaned text is missing the real filing content:\n%s", got)
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
