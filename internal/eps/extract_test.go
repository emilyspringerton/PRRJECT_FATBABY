package eps

import (
	"math"
	"os"
	"testing"
)

func readFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile("../../fixtures/eps/" + name)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(data)
}

func approxEqual(a, b, tol float64) bool {
	return math.Abs(a-b) <= tol
}

// ---- Apple Q1 FY2026 --------------------------------------------------

func TestExtract_AAPL_Q1_2026(t *testing.T) {
	text := readFixture(t, "aapl_q1_2026.txt")
	e := Extract(text, "aapl:test", "AAPL")

	if e.EPSGAAPDiluted == nil {
		t.Fatal("EPSGAAPDiluted is nil, want 2.40")
	}
	if !approxEqual(*e.EPSGAAPDiluted, 2.40, 0.005) {
		t.Errorf("EPSGAAPDiluted = %.4f, want 2.40", *e.EPSGAAPDiluted)
	}
	if e.Period.FiscalQuarter != "Q1" {
		t.Errorf("FiscalQuarter = %q, want Q1", e.Period.FiscalQuarter)
	}
	if e.Period.FiscalYear != 2026 {
		t.Errorf("FiscalYear = %d, want 2026", e.Period.FiscalYear)
	}
	if e.Revenue == nil || !approxEqual(*e.Revenue, 124300, 500) {
		var rev float64
		if e.Revenue != nil {
			rev = *e.Revenue
		}
		t.Errorf("Revenue = %.0f M, want ~124300 M", rev)
	}
	if e.NetIncome == nil || !approxEqual(*e.NetIncome, 36300, 500) {
		var ni float64
		if e.NetIncome != nil {
			ni = *e.NetIncome
		}
		t.Errorf("NetIncome = %.0f M, want ~36300 M", ni)
	}
	if e.ExtractionConfidence < 0.70 {
		t.Errorf("confidence = %.2f, want >= 0.70", e.ExtractionConfidence)
	}
	if e.HasFlag(FlagLowConfidence) {
		t.Error("unexpected FlagLowConfidence for well-formed AAPL release")
	}
}

func TestExtract_AAPL_YoY(t *testing.T) {
	text := readFixture(t, "aapl_q1_2026.txt")
	e := Extract(text, "aapl:test", "AAPL")

	if e.YoYEPS == nil {
		t.Fatal("YoYEPS is nil, want 2.18")
	}
	if !approxEqual(*e.YoYEPS, 2.18, 0.005) {
		t.Errorf("YoYEPS = %.4f, want 2.18", *e.YoYEPS)
	}
	// YoY must differ from current EPS.
	if e.EPSGAAPDiluted != nil && approxEqual(*e.YoYEPS, *e.EPSGAAPDiluted, 0.001) {
		t.Error("YoYEPS equals current EPS — prior-period contamination")
	}
}

func TestExtract_AAPL_Issuer(t *testing.T) {
	text := readFixture(t, "aapl_q1_2026.txt")
	e := Extract(text, "aapl:test", "AAPL")
	if e.Issuer == "" {
		t.Error("Issuer is empty")
	}
}

// ---- Microsoft Q2 FY2026 ---------------------------------------------

func TestExtract_MSFT_Q2_2026(t *testing.T) {
	text := readFixture(t, "msft_q2_2026.txt")
	e := Extract(text, "msft:test", "MSFT")

	if e.EPSGAAPDiluted == nil {
		t.Fatal("EPSGAAPDiluted is nil, want 3.23")
	}
	if !approxEqual(*e.EPSGAAPDiluted, 3.23, 0.005) {
		t.Errorf("EPSGAAPDiluted = %.4f, want 3.23", *e.EPSGAAPDiluted)
	}
	if e.Period.FiscalQuarter != "Q2" {
		t.Errorf("FiscalQuarter = %q, want Q2", e.Period.FiscalQuarter)
	}
}

func TestExtract_MSFT_AdjustedNotHeadline(t *testing.T) {
	text := readFixture(t, "msft_q2_2026.txt")
	e := Extract(text, "msft:test", "MSFT")

	// GAAP EPS ($3.23) must be the headline, not the non-GAAP adjusted ($3.45).
	if e.EPSGAAPDiluted == nil || !approxEqual(*e.EPSGAAPDiluted, 3.23, 0.005) {
		t.Error("EPSGAAPDiluted should be 3.23 (GAAP), not the adjusted 3.45")
	}
	if e.EPSAdjusted != nil && !approxEqual(*e.EPSAdjusted, 3.45, 0.005) {
		t.Errorf("EPSAdjusted = %.4f, want 3.45", *e.EPSAdjusted)
	}
	// GAAP figure must not have non-GAAP value.
	if e.EPSGAAPDiluted != nil && approxEqual(*e.EPSGAAPDiluted, 3.45, 0.005) {
		t.Error("EPSGAAPDiluted grabbed non-GAAP adjusted EPS — label discrimination failed")
	}
}

func TestExtract_MSFT_Revenue(t *testing.T) {
	text := readFixture(t, "msft_q2_2026.txt")
	e := Extract(text, "msft:test", "MSFT")

	if e.Revenue == nil || !approxEqual(*e.Revenue, 69600, 500) {
		var rev float64
		if e.Revenue != nil {
			rev = *e.Revenue
		}
		t.Errorf("Revenue = %.0f M, want ~69600 M", rev)
	}
}

// ---- NVDA Q3 FY2026 --------------------------------------------------

func TestExtract_NVDA_Q3_2026(t *testing.T) {
	text := readFixture(t, "nvda_q3_2026.txt")
	e := Extract(text, "nvda:test", "NVDA")

	if e.EPSGAAPDiluted == nil {
		t.Fatal("EPSGAAPDiluted is nil, want 0.78")
	}
	if !approxEqual(*e.EPSGAAPDiluted, 0.78, 0.005) {
		t.Errorf("EPSGAAPDiluted = %.4f, want 0.78", *e.EPSGAAPDiluted)
	}
	if e.Period.FiscalQuarter != "Q3" {
		t.Errorf("FiscalQuarter = %q, want Q3", e.Period.FiscalQuarter)
	}
}

func TestExtract_NVDA_NotYoY(t *testing.T) {
	text := readFixture(t, "nvda_q3_2026.txt")
	e := Extract(text, "nvda:test", "NVDA")

	// The current EPS is $0.78; prior year was $0.40.
	// Make sure EPSGAAPDiluted is not the prior-year $0.40.
	if e.EPSGAAPDiluted != nil && approxEqual(*e.EPSGAAPDiluted, 0.40, 0.005) {
		t.Error("EPSGAAPDiluted grabbed prior-year figure 0.40 instead of current 0.78")
	}
}

// ---- Loss example ----------------------------------------------------

func TestExtract_Loss_Sign(t *testing.T) {
	text := readFixture(t, "loss_example.txt")
	e := Extract(text, "acme:test", "ACME")

	if e.EPSGAAPDiluted == nil {
		t.Fatal("EPSGAAPDiluted is nil, want -0.42")
	}
	if !approxEqual(*e.EPSGAAPDiluted, -0.42, 0.005) {
		t.Errorf("EPSGAAPDiluted = %.4f, want -0.42 (loss)", *e.EPSGAAPDiluted)
	}
}

func TestValidate_Loss_SignGate_Clean(t *testing.T) {
	text := readFixture(t, "loss_example.txt")
	e := Extract(text, "acme:test", "ACME")
	flags := Validate(e, text)

	// A proper loss release should NOT fire FlagSignMismatch.
	for _, f := range flags {
		if f == FlagSignMismatch {
			t.Error("FlagSignMismatch fired for a correctly-signed loss release")
		}
	}
}

// ---- Validate: trace gate --------------------------------------------

func TestValidate_TraceGate_Pass(t *testing.T) {
	text := readFixture(t, "aapl_q1_2026.txt")
	e := Extract(text, "aapl:test", "AAPL")
	Validate(e, text)

	if e.HasFlag(FlagTraceFailure) {
		t.Error("FlagTraceFailure should not fire when the EPS number appears in the source text")
	}
}

func TestValidate_TraceGate_Fail(t *testing.T) {
	text := readFixture(t, "aapl_q1_2026.txt")
	e := Extract(text, "aapl:test", "AAPL")
	// Force a value that's not in the text.
	bogus := 99.99
	e.EPSGAAPDiluted = &bogus

	Validate(e, text)
	if !e.HasFlag(FlagTraceFailure) {
		t.Error("FlagTraceFailure should fire when EPS is not traceable to source text")
	}
}

// ---- Validate: reconcile gate ----------------------------------------

func TestValidate_ReconcileGate_Pass(t *testing.T) {
	text := readFixture(t, "aapl_q1_2026.txt")
	e := Extract(text, "aapl:test", "AAPL")
	Validate(e, text)

	// AAPL: 36300M / 15123M ≈ 2.40 — should pass.
	if e.HasFlag(FlagReconcileFail) {
		t.Error("reconcile gate should pass for AAPL Q1 2026 (36300 / 15123 ≈ 2.40)")
	}
}

func TestValidate_ReconcileGate_Fail(t *testing.T) {
	eps := 2.40
	ni := 36300.0
	// Inject obviously wrong shares so reconcile fails.
	shares := 5000.0 // 36300/5000 = 7.26 ≠ 2.40
	e := &EarningsData{
		EPSGAAPDiluted: &eps,
		NetIncome:      &ni,
		SharesDiluted:  &shares,
	}
	reconcileGate(e)
	if !e.HasFlag(FlagReconcileFail) {
		t.Error("reconcile gate should fail when net_income/shares does not match eps")
	}
}

// ---- HeadlineEPS / OracleCase helpers --------------------------------

func TestHeadlineEPS_GAAP(t *testing.T) {
	eps := 2.40
	e := &EarningsData{EPSGAAPDiluted: &eps}
	v, isGAAP := e.HeadlineEPS()
	if !approxEqual(v, 2.40, 0.001) || !isGAAP {
		t.Errorf("HeadlineEPS() = (%.2f, %v), want (2.40, true)", v, isGAAP)
	}
}

func TestHeadlineEPS_AdjustedFallback(t *testing.T) {
	adj := 3.45
	e := &EarningsData{EPSAdjusted: &adj}
	v, isGAAP := e.HeadlineEPS()
	if !approxEqual(v, 3.45, 0.001) || isGAAP {
		t.Errorf("HeadlineEPS() = (%.2f, %v), want (3.45, false)", v, isGAAP)
	}
}

func TestHeadlineEPS_Empty(t *testing.T) {
	e := &EarningsData{}
	v, _ := e.HeadlineEPS()
	if v != 0 {
		t.Errorf("HeadlineEPS() = %.2f, want 0 for empty EarningsData", v)
	}
}

// ---- normalizeDate ---------------------------------------------------

func TestNormalizeDate(t *testing.T) {
	cases := []struct{ in, want string }{
		{"March 31, 2026", "2026-03-31"},
		{"December 28, 2025", "2025-12-28"},
		{"October 27, 2025", "2025-10-27"},
		{"2026-03-31", "2026-03-31"},
		{"3/31/2026", "2026-03-31"},
	}
	for _, tc := range cases {
		got := normalizeDate(tc.in)
		if got != tc.want {
			t.Errorf("normalizeDate(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// ---- formatEPS -------------------------------------------------------

func TestFormatEPS(t *testing.T) {
	cases := []struct {
		v    float64
		want string
	}{
		{2.40, "2.40"},
		{-0.42, "-0.42"},
		{0.78, "0.78"},
		{3.23, "3.23"},
		{10.00, "10.00"},
	}
	for _, tc := range cases {
		got := formatEPS(tc.v)
		if got != tc.want {
			t.Errorf("formatEPS(%.2f) = %q, want %q", tc.v, got, tc.want)
		}
	}
}
