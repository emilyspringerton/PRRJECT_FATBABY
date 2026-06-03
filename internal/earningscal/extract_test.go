package earningscal

import "testing"

func TestExtractDate_LongFormDate(t *testing.T) {
	cases := []struct {
		text string
		want string
	}{
		{
			"Company will report Q3 2026 results on November 14, 2026.",
			"2026-11-14",
		},
		{
			"Earnings release scheduled for March 5, 2026 after the close.",
			"2026-03-05",
		},
		{
			"Conference call on August 7th, 2026 at 9:00 AM.",
			"2026-08-07",
		},
		{
			"To report quarterly results on Oct 28, 2027.",
			"2027-10-28",
		},
	}
	for _, tc := range cases {
		got, ok := ExtractDate(tc.text)
		if !ok {
			t.Errorf("ExtractDate(%q): expected ok=true, got false", tc.text)
			continue
		}
		if got != tc.want {
			t.Errorf("ExtractDate(%q) = %q, want %q", tc.text, got, tc.want)
		}
	}
}

func TestExtractDate_ISODate(t *testing.T) {
	text := "The company will release earnings on 2026-10-22."
	got, ok := ExtractDate(text)
	if !ok {
		t.Fatal("expected ok=true for ISO date")
	}
	if got != "2026-10-22" {
		t.Errorf("got %q, want 2026-10-22", got)
	}
}

func TestExtractDate_SlashDate(t *testing.T) {
	text := "Earnings call scheduled for 2/14/2027."
	got, ok := ExtractDate(text)
	if !ok {
		t.Fatal("expected ok=true for slash date")
	}
	if got != "2027-02-14" {
		t.Errorf("got %q, want 2027-02-14", got)
	}
}

func TestExtractDate_NoMatch(t *testing.T) {
	cases := []string{
		"The company posted strong revenue growth this quarter.",
		"",
		"John will call on Tuesday.",
	}
	for _, text := range cases {
		_, ok := ExtractDate(text)
		if ok {
			t.Errorf("ExtractDate(%q): expected ok=false (no earnings language), got true", text)
		}
	}
}

func TestExtractPeriod_Quarters(t *testing.T) {
	cases := []struct {
		text    string
		quarter string
		year    int
	}{
		{"Reporting Q1 2026 results next month.", "Q1", 2026},
		{"Third quarter earnings call on August 7.", "Q3", 0},
		{"Full-year fiscal 2027 guidance.", "FY", 2027},
		{"4Q results to be reported in February.", "Q4", 0},
	}
	for _, tc := range cases {
		q, y := ExtractPeriod(tc.text)
		if q != tc.quarter {
			t.Errorf("ExtractPeriod(%q) quarter = %q, want %q", tc.text, q, tc.quarter)
		}
		if y != tc.year {
			t.Errorf("ExtractPeriod(%q) year = %d, want %d", tc.text, y, tc.year)
		}
	}
}

func TestExtractBeforeMarket(t *testing.T) {
	bmo := ExtractBeforeMarket("Results before the market open on Thursday.")
	if bmo == nil || !*bmo {
		t.Error("expected BMO=true for 'before the market open'")
	}
	amc := ExtractBeforeMarket("Report will be released after market close.")
	if amc == nil || *amc {
		t.Error("expected AMC=false for 'after market close'")
	}
	unknown := ExtractBeforeMarket("Results available on Thursday morning.")
	if unknown != nil {
		t.Error("expected nil for unknown timing")
	}
}

func TestMakeID(t *testing.T) {
	id := MakeID("AAPL", "Q3", 2026)
	if id != "earncal-AAPL-Q3-2026" {
		t.Errorf("MakeID = %q, want earncal-AAPL-Q3-2026", id)
	}
}
