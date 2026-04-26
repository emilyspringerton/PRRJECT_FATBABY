package secwatch

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseRecentFilings_FromFixture(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "fixtures", "AAPL_0000320193", "submissions.json"))
	if err != nil {
		t.Fatal(err)
	}
	filings, err := ParseRecentFilings(b, "AAPL")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(filings) == 0 {
		t.Fatal("expected filings")
	}
	if filings[0].CIK != "0000320193" {
		t.Fatalf("unexpected cik %q", filings[0].CIK)
	}
	if filings[0].SubmissionsURL == "" {
		t.Fatal("expected submissions url")
	}
}

func TestFilterByAllowedForms(t *testing.T) {
	in := []Filing{{Form: "8-K"}, {Form: "10-Q"}, {Form: "S-8"}}
	out := FilterByAllowedForms(in, []string{"10-Q", "8-K"})
	if len(out) != 2 {
		t.Fatalf("expected 2 got=%d", len(out))
	}
}

func TestParseRecentFilings_LengthMismatch(t *testing.T) {
	payload := []byte(`{"cik":"1","filings":{"recent":{"accessionNumber":["a","b"],"form":["8-K"],"filingDate":["2026-01-01"],"primaryDocument":["x.htm"]}}}`)
	if _, err := ParseRecentFilings(payload, "X"); err == nil {
		t.Fatal("expected error")
	}
}
