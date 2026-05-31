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

func TestSubmissionsPageNames(t *testing.T) {
	payload := []byte(`{
		"cik": "0000019617",
		"filings": {
			"recent": {"accessionNumber":[],"form":[],"filingDate":[],"primaryDocument":[]},
			"files": [
				{"name": "CIK0000019617-submissions-001.json"},
				{"name": "CIK0000019617-submissions-002.json"}
			]
		}
	}`)
	names, err := SubmissionsPageNames(payload)
	if err != nil {
		t.Fatalf("SubmissionsPageNames: %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("expected 2 page names got %d", len(names))
	}
	if names[0] != "CIK0000019617-submissions-001.json" {
		t.Errorf("unexpected first page name %q", names[0])
	}
}

func TestParseFilingsPage(t *testing.T) {
	// A submissions page has the same shape as filings.recent but without the outer wrapper.
	payload := []byte(`{
		"accessionNumber": ["0000019617-10-001234"],
		"form":            ["8-K"],
		"filingDate":      ["2010-03-15"],
		"primaryDocument": ["d8k.htm"]
	}`)
	filings, err := ParseFilingsPage(payload, "0000019617", "JPM")
	if err != nil {
		t.Fatalf("ParseFilingsPage: %v", err)
	}
	if len(filings) != 1 {
		t.Fatalf("expected 1 filing got %d", len(filings))
	}
	if filings[0].Ticker != "JPM" {
		t.Errorf("Ticker got %q want JPM", filings[0].Ticker)
	}
	if filings[0].FilingDate != "2010-03-15" {
		t.Errorf("FilingDate got %q want 2010-03-15", filings[0].FilingDate)
	}
}

func TestParseRecentFilings_PrimaryDocumentIsFullURL(t *testing.T) {
	payload := []byte(`{
        "cik": "1321655",
        "filings": {"recent": {
            "accessionNumber": ["0001193125-22-144264"],
            "form":            ["8-K"],
            "filingDate":      ["2022-05-10"],
            "primaryDocument": ["d259921d8k.htm"]
        }}
    }`)
	filings, err := ParseRecentFilings(payload, "PLTR")
	if err != nil {
		t.Fatal(err)
	}
	if len(filings) != 1 {
		t.Fatalf("expected 1 filing got %d", len(filings))
	}
	want := "https://www.sec.gov/Archives/edgar/data/1321655/000119312522144264/d259921d8k.htm"
	if filings[0].PrimaryDocument != want {
		t.Errorf("PrimaryDocument\n got  %q\n want %q", filings[0].PrimaryDocument, want)
	}
	if filings[0].Ticker != "PLTR" {
		t.Errorf("Ticker got %q want PLTR", filings[0].Ticker)
	}
}
