package main

import (
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/example/prrject-fatbaby/internal/movers"
	"github.com/example/prrject-fatbaby/secwatch"
)

func TestBuildArticleBody_SortsByAbsChangePercentDescending(t *testing.T) {
	snap := movers.Snapshot{
		Gainers: []movers.Quote{
			{Symbol: "SMALL", Name: "Small Co", ChangePercent: 2.0},
			{Symbol: "BIG", Name: "Big Co", ChangePercent: 15.0},
			{Symbol: "MID", Name: "Mid Co", ChangePercent: 7.5},
		},
	}
	body := buildArticleBody(snap, nil, nil, time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC), "")

	iBig := strings.Index(body, "BIG")
	iMid := strings.Index(body, "MID")
	iSmall := strings.Index(body, "SMALL")
	if !(iBig < iMid && iMid < iSmall) {
		t.Errorf("expected order BIG, MID, SMALL by descending |change%%|; got positions %d, %d, %d", iBig, iMid, iSmall)
	}
}

func TestBuildArticleBody_FlagsTrackedTickers(t *testing.T) {
	snap := movers.Snapshot{
		Gainers: []movers.Quote{
			{Symbol: "AAPL", Name: "Apple Inc.", ChangePercent: 3.0},
			{Symbol: "RANDOM", Name: "Random Co", ChangePercent: 4.0},
		},
	}
	tracked := map[string]bool{"AAPL": true}
	body := buildArticleBody(snap, tracked, nil, time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC), "")

	var appleLine, randomLine string
	for _, line := range strings.Split(body, "\n") {
		switch {
		case strings.Contains(line, "AAPL"):
			appleLine = line
		case strings.Contains(line, "RANDOM"):
			randomLine = line
		}
	}
	if !strings.Contains(appleLine, "(tracked") {
		t.Errorf("expected AAPL line to be flagged as tracked, got: %q", appleLine)
	}
	if strings.Contains(randomLine, "(tracked") {
		t.Errorf("RANDOM should not be flagged as tracked, got: %q", randomLine)
	}
}

func TestBuildArticleBody_HandlesEmptySections(t *testing.T) {
	body := buildArticleBody(movers.Snapshot{}, nil, nil, time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC), "")
	if !strings.Contains(body, "No qualifying names today.") {
		t.Errorf("expected graceful empty-section message, body:\n%s", body)
	}
	if !strings.Contains(body, "TOP GAINERS") || !strings.Contains(body, "TOP LOSERS") {
		t.Error("expected both section headers even when empty")
	}
}

func TestBuildArticle_HeadlineAndKind(t *testing.T) {
	snap := movers.Snapshot{Gainers: []movers.Quote{{Symbol: "X", ChangePercent: 1}}}
	art := buildArticle(snap, nil, nil, time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC), "https://news.okemily.com", "")

	if art["kind"] != "market_movers" {
		t.Errorf("kind = %v, want market_movers", art["kind"])
	}
	headline, _ := art["headline"].(string)
	if !strings.Contains(headline, "Stocks on the Move") || !strings.Contains(headline, "2026") {
		t.Errorf("headline = %q, missing expected content", headline)
	}
	id, _ := art["id"].(string)
	if id != "movers-2026-07-20" {
		t.Errorf("id = %q, want movers-2026-07-20", id)
	}
}

func TestBuildArticle_BodyHTML_HasRealAbsoluteTickerLinks(t *testing.T) {
	snap := movers.Snapshot{
		Gainers: []movers.Quote{{Symbol: "aapl", Name: "Apple Inc.", Exchange: "NasdaqGS", ChangePercent: 3.5}},
	}
	art := buildArticle(snap, nil, nil, time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC), "https://news.okemily.com", "")

	bodyHTML, _ := art["body_html"].(string)
	if bodyHTML == "" {
		t.Fatal("expected body_html to be set")
	}
	if !strings.Contains(bodyHTML, `<a href="https://news.okemily.com/ticker/AAPL">AAPL</a>`) {
		t.Errorf("expected a real absolute ticker link per the editorial standard, got:\n%s", bodyHTML)
	}
	if !strings.Contains(bodyHTML, "Apple Inc. (NASDAQ:") {
		t.Errorf("expected 'Company Name (EXCHANGE:...)' format with normalized exchange, got:\n%s", bodyHTML)
	}
}

func TestBuildArticleBody_PlainTextHasNoMarkup(t *testing.T) {
	snap := movers.Snapshot{
		Gainers: []movers.Quote{{Symbol: "AAPL", Name: "Apple Inc.", Exchange: "NYSE", ChangePercent: 3.5}},
	}
	body := buildArticleBody(snap, nil, nil, time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC), "")
	if strings.Contains(body, "<a ") {
		t.Errorf("plain-text body must not contain HTML markup, got:\n%s", body)
	}
	if !strings.Contains(body, "Apple Inc. (NYSE:AAPL)") {
		t.Errorf("expected plain 'Company Name (EXCHANGE:TICKER)' format, got:\n%s", body)
	}
}

func TestNormalizeExchange(t *testing.T) {
	cases := map[string]string{
		"NasdaqGS":     "NASDAQ",
		"NasdaqGM":     "NASDAQ",
		"NYSE":         "NYSE",
		"NYSEArca":     "NYSE Arca",
		"NYSEAmerican": "NYSE American",
		"":             "",
		"SomeNewExch":  "SOMENEWEXCH",
	}
	for in, want := range cases {
		if got := normalizeExchange(in); got != want {
			t.Errorf("normalizeExchange(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMintTrackedSkuldmarks_OnlyMintsForCompleteEntries(t *testing.T) {
	wl := secwatch.Watchlist{Entries: []secwatch.WatchEntry{
		{Ticker: "AAPL", CIK: "320193", Exchange: "Nasdaq", Enabled: true},
		{Ticker: "NOEXCH", CIK: "123456", Enabled: true},         // no Exchange -- must not mint
		{Ticker: "DISABLED", CIK: "999999", Exchange: "NYSE"},    // not Enabled -- must not mint
	}}
	logger := log.New(os.Stderr, "", 0)
	got := mintTrackedSkuldmarks(wl, logger)

	id, ok := got["AAPL"]
	if !ok || id == "" {
		t.Fatalf("expected a real minted SKULDMARK ID for AAPL, got %+v", got)
	}
	if len(id) != 25 {
		t.Errorf("expected a 25-char SKULDMARK-25 ID, got %q (%d chars)", id, len(id))
	}
	if _, ok := got["NOEXCH"]; ok {
		t.Errorf("expected no ID minted for an entry missing Exchange, got %+v", got)
	}
	if _, ok := got["DISABLED"]; ok {
		t.Errorf("expected no ID minted for a disabled entry, got %+v", got)
	}
}

func TestBuildArticle_SlotProducesADistinctIDAndHeadline(t *testing.T) {
	snap := movers.Snapshot{Gainers: []movers.Quote{{Symbol: "X", ChangePercent: 1}}}
	now := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)

	morning := buildArticle(snap, nil, nil, now, "https://news.okemily.com", "")
	midday := buildArticle(snap, nil, nil, now, "https://news.okemily.com", "Midday")

	if morning["id"] == midday["id"] {
		t.Fatalf("expected a slotted run to get a distinct article ID (commentary dedups by exact ID, last-write-wins), got the same id %v for both", morning["id"])
	}
	if midday["id"] != "movers-2026-07-20-midday" {
		t.Errorf("id = %v, want movers-2026-07-20-midday", midday["id"])
	}
	middayHeadline, _ := midday["headline"].(string)
	if !strings.Contains(middayHeadline, "Midday") {
		t.Errorf("expected the midday headline to say so, got %q", middayHeadline)
	}
}

func TestBuildArticleBodyHTML_IncludesSkuldmarkTagForTrackedNames(t *testing.T) {
	snap := movers.Snapshot{Gainers: []movers.Quote{{Symbol: "AAPL", Name: "Apple Inc.", Exchange: "Nasdaq", ChangePercent: 3.0}}}
	tracked := map[string]bool{"AAPL": true}
	skuldmarks := map[string]string{"AAPL": "EINXNASAAPLXXX0000320193K"}
	bodyHTML := buildArticleBodyHTML(snap, tracked, skuldmarks, time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC), "https://news.okemily.com")
	if !strings.Contains(bodyHTML, `data-skuldmark="EINXNASAAPLXXX0000320193K"`) {
		t.Errorf("expected the SKULDMARK tag to be rendered for a tracked name with a minted ID, got:\n%s", bodyHTML)
	}
}
