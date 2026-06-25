package earningscal

import "testing"

var testEntries = [][2]string{
	{"AAPL", "Apple Inc."},
	{"MSFT", "Microsoft Corporation"},
	{"JPM", "JPMorgan Chase & Co."},
	{"BRK.B", "Berkshire Hathaway Inc."},
	{"FHN", "First Horizon Corporation"},
	{"NVDA", "NVIDIA Corporation"},
}

func TestResolveExact(t *testing.T) {
	r := NewCompanyResolver(testEntries)
	cases := []struct{ name, want string }{
		{"Apple Inc.", "AAPL"},
		{"Apple Inc", "AAPL"},
		{"apple inc.", "AAPL"},
		{"Microsoft Corporation", "MSFT"},
		{"First Horizon Corporation", "FHN"},
	}
	for _, tc := range cases {
		if got := r.Resolve(tc.name); got != tc.want {
			t.Errorf("Resolve(%q)=%q want %q", tc.name, got, tc.want)
		}
	}
}

func TestResolveDirectTicker(t *testing.T) {
	r := NewCompanyResolver(testEntries)
	if got := r.Resolve("AAPL"); got != "AAPL" {
		t.Errorf("Resolve(AAPL)=%q", got)
	}
}

func TestResolveNoMatch(t *testing.T) {
	r := NewCompanyResolver(testEntries)
	if got := r.Resolve("M3-Brigade Acquisition V Corp."); got != "" {
		t.Errorf("expected no match, got %q", got)
	}
}

func TestCleanIssuerFn(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{
			"16:17 ET\n\t\t\t\n\t\t\t\n\t\t\t\tFirst Horizon Corporation",
			"First Horizon Corporation",
		},
		{
			"08:34 ET\n\t\t\t\n\t\t\t\n\t\t\t\tApple Inc.",
			"Apple Inc.",
		},
		{
			"Apple Inc.", // no prefix
			"Apple Inc.",
		},
		{
			"16:15 ET\n\t\t\t\n\t\t\t\tGPK Deadline Alert: Levi & Korsinsky...",
			"GPK Deadline Alert: Levi & Korsinsky...",
		},
	}
	for _, tc := range cases {
		if got := CleanIssuer(tc.raw); got != tc.want {
			t.Errorf("cleanIssuer(%q)=%q want %q", tc.raw, got, tc.want)
		}
	}
}

func TestResolveAfterClean(t *testing.T) {
	r := NewCompanyResolver(testEntries)
	// Simulates what comes out of the PR body parser.
	raw := "16:15 ET\n\t\t\t\n\t\t\t\n\t\t\t\tFirst Horizon Corporation to Announce Second Quarter Financial Results"
	// cleanIssuer will get us "First Horizon Corporation to Announce Second Quarter Financial Results"
	// which should prefix-match "first horizon corporation"
	got := r.Resolve(raw)
	if got != "FHN" {
		t.Errorf("Resolve(raw issuer)=%q want FHN", got)
	}
}
