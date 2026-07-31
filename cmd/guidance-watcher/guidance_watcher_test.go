package main

import "testing"

func TestExtractIssuerFromTitle(t *testing.T) {
	cases := []struct {
		title string
		want  string
	}{
		{"Apple Inc. Reports Strong Q3 Results", "Apple Inc."},
		{"Microsoft Announces Record Revenue", "Microsoft"},
		{"Schwab Provides 2026 Guidance Update", "Schwab"},
		{"Tesla Issues Q2 Earnings Report", "Tesla"},
		{"Goldman Sachs Updates Full-Year Outlook", "Goldman Sachs"},
		// No keyword — fallback to first 40 chars.
		{"A very long company name that exceeds forty characters here", "A very long company name that exceeds fo"},
		// Short title without keyword.
		{"Short Title", "Short Title"},
		{"", ""},
	}
	for _, tc := range cases {
		got := extractIssuerFromTitle(tc.title)
		if got != tc.want {
			t.Errorf("extractIssuerFromTitle(%q) = %q, want %q", tc.title, got, tc.want)
		}
	}
}

func TestExtractIssuerFromTitle_TrimsSpace(t *testing.T) {
	// Leading/trailing spaces should be trimmed.
	got := extractIssuerFromTitle("  Company Name  Reports Results")
	if got != "Company Name" {
		t.Errorf("expected trimmed issuer, got %q", got)
	}
}

// S170-07: real ev.Headline captured live from var/guidance/articles.ndjson --
// prwatch's discovery scraper carries a leading "HH:MM ET" timestamp artifact
// that garbled every issuer this pipeline had ever extracted, worst-case on
// the 40-char fallback (see internal/prspam.StripTimePrefix's own doc
// comment). Litigation-alert detection itself is covered directly in
// internal/prspam/prspam_test.go, shared with dividend-watcher and
// earnings-calendar.
func TestExtractIssuerFromTitle_StripsTimePrefix(t *testing.T) {
	cases := []struct {
		title string
		want  string
	}{
		{"16:30 ET\n\t\t\t\n\t\t\t\n\t\t\t\tNucor", "Nucor"},
		{"06:00 ET\n\t\t\t\n\t\t\t\n\t\t\t\tDanaher", "Danaher"},
		{"06:30 ET\n\t\t\t\n\t\t\t\n\t\t\t\tColumbus McKinnon Reports Order Growth of 20%", "Columbus McKinnon"},
	}
	for _, tc := range cases {
		got := extractIssuerFromTitle(tc.title)
		if got != tc.want {
			t.Errorf("extractIssuerFromTitle(%q) = %q, want %q", tc.title, got, tc.want)
		}
	}
}
