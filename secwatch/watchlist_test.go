package secwatch

import (
	"path/filepath"
	"testing"
)

func TestLoadWatchlist(t *testing.T) {
	w, err := LoadWatchlist(filepath.Join("..", "config", "watchlist.json"))
	if err != nil {
		t.Fatalf("load watchlist: %v", err)
	}
	if len(w.Entries) == 0 {
		t.Fatal("expected entries")
	}
	if got := w.Entries[0].CIK; len(got) != 10 {
		t.Fatalf("expected normalized cik len=10 got=%q", got)
	}
}

func TestNormalizeCIK(t *testing.T) {
	cases := map[string]string{
		"320193":       "0000320193",
		"0000320193":   "0000320193",
		" cik 789019 ": "0000789019",
		"":             "",
		"abc":          "",
	}
	for in, want := range cases {
		if got := NormalizeCIK(in); got != want {
			t.Fatalf("normalize %q got=%q want=%q", in, got, want)
		}
	}
}
