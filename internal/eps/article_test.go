package eps

import (
	"strings"
	"testing"
)

func TestGenerate_AAPL(t *testing.T) {
	text := readFixture(t, "aapl_q1_2026.txt")
	e := Extract(text, "aapl:test", "AAPL")
	Validate(e, text)

	art, ok := Generate(e)
	if !ok {
		t.Fatalf("Generate returned false for AAPL Q1 2026 (confidence=%.2f, flags=%v)", e.ExtractionConfidence, e.ExtractionFlags)
	}
	if art.IsGAAP != true {
		t.Error("IsGAAP should be true for AAPL GAAP EPS")
	}
	if !strings.Contains(art.Headline, "2.40") {
		t.Errorf("Headline %q should contain 2.40", art.Headline)
	}
	if !strings.Contains(art.Headline, "Q1 2026") {
		t.Errorf("Headline %q should contain Q1 2026", art.Headline)
	}
	if !strings.Contains(art.Headline, "reports") {
		t.Errorf("Headline %q should contain 'reports'", art.Headline)
	}
	if art.Ticker != "AAPL" {
		t.Errorf("Ticker = %q, want AAPL", art.Ticker)
	}
	if art.PublishAt == "" {
		t.Error("PublishAt should not be empty")
	}
}

func TestGenerate_MSFT_GAAPNotAdjusted(t *testing.T) {
	text := readFixture(t, "msft_q2_2026.txt")
	e := Extract(text, "msft:test", "MSFT")
	Validate(e, text)

	art, ok := Generate(e)
	if !ok {
		t.Fatalf("Generate returned false for MSFT Q2 2026")
	}
	// Headline must use GAAP EPS ($3.23), not non-GAAP ($3.45).
	if strings.Contains(art.Headline, "3.45") {
		t.Error("Headline should not contain non-GAAP EPS 3.45")
	}
	if !strings.Contains(art.Headline, "3.23") {
		t.Errorf("Headline %q should contain GAAP EPS 3.23", art.Headline)
	}
	if !art.IsGAAP {
		t.Error("IsGAAP should be true when GAAP EPS is present")
	}
}

func TestGenerate_Loss(t *testing.T) {
	text := readFixture(t, "loss_example.txt")
	e := Extract(text, "acme:test", "ACME")
	Validate(e, text)

	art, ok := Generate(e)
	if !ok {
		t.Fatalf("Generate returned false for loss example")
	}
	// Loss headline format: "... loss of $0.42 per share"
	if !strings.Contains(art.Headline, "loss") {
		t.Errorf("loss headline %q should contain 'loss'", art.Headline)
	}
	if !strings.Contains(art.Headline, "0.42") {
		t.Errorf("loss headline %q should contain 0.42", art.Headline)
	}
	if art.EPSValue >= 0 {
		t.Errorf("EPSValue = %.2f, want negative for loss", art.EPSValue)
	}
}

func TestGenerate_LowConfidence_Blocked(t *testing.T) {
	// An EarningsData with low confidence should not publish.
	lowConf := 0.50
	eps := 1.23
	e := &EarningsData{
		SourceIdentity:       "test:001",
		Ticker:               "TST",
		Issuer:               "Test Corp",
		EPSGAAPDiluted:       &eps,
		ExtractionConfidence: lowConf,
		ExtractionFlags:      []ExtractionFlag{FlagLowConfidence},
	}
	_, ok := Generate(e)
	if ok {
		t.Error("Generate should return false for low-confidence extraction")
	}
}

func TestGenerate_TraceFailure_Blocked(t *testing.T) {
	eps := 1.23
	e := &EarningsData{
		SourceIdentity:       "test:001",
		Ticker:               "TST",
		Issuer:               "Test Corp",
		EPSGAAPDiluted:       &eps,
		ExtractionConfidence: 0.85,
		ExtractionFlags:      []ExtractionFlag{FlagTraceFailure},
	}
	_, ok := Generate(e)
	if ok {
		t.Error("Generate should return false when FlagTraceFailure is set")
	}
}

func TestFormatPeriodLabel(t *testing.T) {
	cases := []struct {
		p    EarningsPeriod
		want string
	}{
		{EarningsPeriod{FiscalQuarter: "Q1", FiscalYear: 2026}, "Q1 2026"},
		{EarningsPeriod{FiscalQuarter: "FY", FiscalYear: 2026}, "full-year 2026"},
		{EarningsPeriod{FiscalQuarter: "Q3"}, "Q3"},
		{EarningsPeriod{FiscalYear: 2026}, "2026"},
		{EarningsPeriod{}, "current period"},
	}
	for _, tc := range cases {
		got := formatPeriodLabel(tc.p)
		if got != tc.want {
			t.Errorf("formatPeriodLabel(%+v) = %q, want %q", tc.p, got, tc.want)
		}
	}
}

func TestGenerate_Body_ContainsRevenue(t *testing.T) {
	text := readFixture(t, "aapl_q1_2026.txt")
	e := Extract(text, "aapl:test", "AAPL")
	Validate(e, text)

	art, ok := Generate(e)
	if !ok {
		t.Skip("AAPL not publishable")
	}
	// Body should mention revenue.
	if !strings.Contains(art.Body, "Revenue") {
		t.Errorf("body should contain Revenue line; got:\n%s", art.Body)
	}
}
