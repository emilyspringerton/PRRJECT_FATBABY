package processor

import "testing"

// TestSourceTypeForForm is the regression test for the bug found 2026-07-19:
// worker.go used to default every non-8-K SEC filing to source_type
// "press_release", which meant newssite's "/wire" page (filtered to
// source_type=="press_release") showed plain 10-Q filings (NFLX, GE, COST)
// mixed in with real PR Newswire press releases -- this package only ever
// processes SEC EDGAR filings, never real press releases, so that default
// was never correct for anything.
func TestSourceTypeForForm(t *testing.T) {
	cases := []struct{ form, want string }{
		{"8-K", "sec_8k"},
		{"8-K/A", "sec_8k"},
		{"10-Q", "sec_10q"},
		{"10-Q/A", "sec_10q"},
		{"10-K", "sec_10k"},
		{"10-K/A", "sec_10k"},
		{"DEF 14A", "sec_def14a"},
		{"DEFA14A", "sec_def14a"},
		{"4", "sec_form4"},
		{"NT 10-K", "sec_nt10k"},
		{"NT 10-Q", "sec_nt10q"},
		{"10-q", "sec_10q"},   // case-insensitive
		{"  8-K  ", "sec_8k"}, // trims whitespace
		{"S-1", "sec_filing"}, // unrecognized form: generic fallback, never press_release
		{"", "sec_filing"},
	}
	for _, tc := range cases {
		if got := sourceTypeForForm(tc.form); got != tc.want {
			t.Errorf("sourceTypeForForm(%q) = %q, want %q", tc.form, got, tc.want)
		}
	}
}
