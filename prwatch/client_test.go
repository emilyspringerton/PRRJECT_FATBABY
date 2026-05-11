package prwatch

import "testing"

func TestExtractID(t *testing.T) {
	id := extractID("https://www.prnewswire.com/news-releases/acme-launches-widget-302123456.html")
	if id != "302123456" {
		t.Fatalf("got %q", id)
	}
}

func TestParsePRNewswireTime(t *testing.T) {
	tm := parsePRNewswireTime("May 10, 2026, 3:04 PM ET")
	if tm.IsZero() {
		t.Fatal("expected parsed time")
	}
}
