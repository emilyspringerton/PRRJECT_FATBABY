package main

import (
	"strings"
	"testing"
	"time"

	"github.com/example/prrject-fatbaby/internal/movers"
)

func TestBuildArticleBody_SortsByAbsChangePercentDescending(t *testing.T) {
	snap := movers.Snapshot{
		Gainers: []movers.Quote{
			{Symbol: "SMALL", Name: "Small Co", ChangePercent: 2.0},
			{Symbol: "BIG", Name: "Big Co", ChangePercent: 15.0},
			{Symbol: "MID", Name: "Mid Co", ChangePercent: 7.5},
		},
	}
	body := buildArticleBody(snap, nil, time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC))

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
	body := buildArticleBody(snap, tracked, time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC))

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
	body := buildArticleBody(movers.Snapshot{}, nil, time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC))
	if !strings.Contains(body, "No qualifying names today.") {
		t.Errorf("expected graceful empty-section message, body:\n%s", body)
	}
	if !strings.Contains(body, "TOP GAINERS") || !strings.Contains(body, "TOP LOSERS") {
		t.Error("expected both section headers even when empty")
	}
}

func TestBuildArticle_HeadlineAndKind(t *testing.T) {
	snap := movers.Snapshot{Gainers: []movers.Quote{{Symbol: "X", ChangePercent: 1}}}
	art := buildArticle(snap, nil, time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC))

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
