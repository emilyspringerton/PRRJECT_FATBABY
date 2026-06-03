package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPadCIK(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"320193", "0000320193"},           // AAPL
		{"0000320193", "0000320193"},        // already padded
		{"1", "0000000001"},
		{"789019", "0000789019"},            // MSFT
		{"0000000001", "0000000001"},        // already 10 digits
		{"12345678901", "12345678901"},      // over 10 digits — unchanged
	}
	for _, tc := range cases {
		got := padCIK(tc.in)
		if got != tc.want {
			t.Errorf("padCIK(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestParseEFTSResponse_MultipleHits(t *testing.T) {
	raw := &eftsResponse{}
	raw.Hits.Hits = []eftsHit{
		{Source: struct {
			FileDate     string   `json:"file_date"`
			FormType     string   `json:"form_type"`
			EntityName   string   `json:"entity_name"`
			AccessionNo  string   `json:"accession_no"`
			DisplayNames []string `json:"display_names"`
		}{
			FileDate:    "2026-03-15",
			FormType:    "SC 13D",
			EntityName:  "Activist Partners LP",
			AccessionNo: "0001234567-26-000001",
		}},
		{Source: struct {
			FileDate     string   `json:"file_date"`
			FormType     string   `json:"form_type"`
			EntityName   string   `json:"entity_name"`
			AccessionNo  string   `json:"accession_no"`
			DisplayNames []string `json:"display_names"`
		}{
			FileDate:    "2026-04-02",
			FormType:    "SC 13D/A",
			EntityName:  "Activist Partners LP",
			AccessionNo: "0001234567-26-000042",
		}},
	}

	filings := parseEFTSResponse(raw, "SCHW", "0000316206")
	if len(filings) != 2 {
		t.Fatalf("expected 2 filings, got %d", len(filings))
	}
	if filings[0].Ticker != "SCHW" {
		t.Errorf("Ticker = %q, want SCHW", filings[0].Ticker)
	}
	if filings[0].TargetCIK != "0000316206" {
		t.Errorf("TargetCIK = %q, want 0000316206", filings[0].TargetCIK)
	}
	if filings[0].FilingDate != "2026-03-15" {
		t.Errorf("FilingDate = %q, want 2026-03-15", filings[0].FilingDate)
	}
	if filings[0].FilingType != "SC 13D" {
		t.Errorf("FilingType = %q, want SC 13D", filings[0].FilingType)
	}
	if filings[0].Accession != "0001234567-26-000001" {
		t.Errorf("Accession = %q", filings[0].Accession)
	}
	if filings[1].FilingType != "SC 13D/A" {
		t.Errorf("second filing type = %q, want SC 13D/A", filings[1].FilingType)
	}
}

func TestParseEFTSResponse_EmptyHits(t *testing.T) {
	raw := &eftsResponse{}
	filings := parseEFTSResponse(raw, "AAPL", "0000320193")
	if len(filings) != 0 {
		t.Errorf("expected 0 filings for empty response, got %d", len(filings))
	}
}

func TestParseEFTSResponse_SkipsMissingFields(t *testing.T) {
	// A hit with no FormType or FileDate should be skipped.
	raw := &eftsResponse{}
	raw.Hits.Hits = []eftsHit{
		{Source: struct {
			FileDate     string   `json:"file_date"`
			FormType     string   `json:"form_type"`
			EntityName   string   `json:"entity_name"`
			AccessionNo  string   `json:"accession_no"`
			DisplayNames []string `json:"display_names"`
		}{
			EntityName:  "Some Filer",
			AccessionNo: "0001234567-26-000099",
			// FormType and FileDate intentionally empty
		}},
	}
	filings := parseEFTSResponse(raw, "AAPL", "0000320193")
	if len(filings) != 0 {
		t.Errorf("expected incomplete hit to be skipped, got %d filings", len(filings))
	}
}

func TestLoadWatchlist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "watchlist.json")

	data, _ := json.Marshal(map[string]any{
		"entries": []map[string]any{
			{"ticker": "AAPL", "cik": "0000320193", "enabled": true},
			{"ticker": "MSFT", "cik": "0000789019", "enabled": false},
		},
		"version": 1,
	})
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write test watchlist: %v", err)
	}

	wl, err := loadWatchlist(path)
	if err != nil {
		t.Fatalf("loadWatchlist: %v", err)
	}
	if len(wl.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(wl.Entries))
	}
	if wl.Entries[0].Ticker != "AAPL" || !wl.Entries[0].Enabled {
		t.Errorf("first entry: %+v, want AAPL enabled", wl.Entries[0])
	}
	if wl.Entries[1].Ticker != "MSFT" || wl.Entries[1].Enabled {
		t.Errorf("second entry: %+v, want MSFT disabled", wl.Entries[1])
	}
}

func TestLoadWatchlist_MissingFile(t *testing.T) {
	_, err := loadWatchlist("/nonexistent/watchlist.json")
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

func TestParseEFTSResponse_RoundTrip(t *testing.T) {
	// Validate that a realistic JSON payload parses to the correct filing shape.
	rawJSON := `{
		"hits": {
			"total": {"value": 1},
			"hits": [{
				"_source": {
					"file_date": "2026-05-20",
					"form_type": "SC 13G",
					"entity_name": "Vanguard Group Inc",
					"accession_no": "0000102909-26-001234",
					"display_names": ["Vanguard Group Inc"]
				}
			}]
		}
	}`
	var result eftsResponse
	if err := json.Unmarshal([]byte(rawJSON), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	filings := parseEFTSResponse(&result, "SCHW", "0000316206")
	if len(filings) != 1 {
		t.Fatalf("expected 1 filing, got %d", len(filings))
	}
	f := filings[0]
	if f.FilingType != "SC 13G" {
		t.Errorf("FilingType = %q, want SC 13G", f.FilingType)
	}
	if f.FilerName != "Vanguard Group Inc" {
		t.Errorf("FilerName = %q, want Vanguard Group Inc", f.FilerName)
	}
	if f.FilingDate != "2026-05-20" {
		t.Errorf("FilingDate = %q, want 2026-05-20", f.FilingDate)
	}
}
