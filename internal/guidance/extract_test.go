package guidance

import (
	"testing"
	"time"
)

func TestExtract_EPSGuidanceRange(t *testing.T) {
	text := `ACME Corp reports Q3 results. Looking ahead, the Company expects full-year EPS
of $4.50 to $4.75 per diluted share, raising its previously issued guidance.`
	g := Extract(text, "identity-001", "ACME")
	if !g.HasGuidance {
		t.Fatal("expected HasGuidance=true")
	}
	if g.Action != ActionRaised {
		t.Errorf("action = %q, want raised", g.Action)
	}
	if g.EPSLow == nil || *g.EPSLow != 4.50 {
		t.Errorf("EPSLow = %v, want 4.50", g.EPSLow)
	}
	if g.EPSHigh == nil || *g.EPSHigh != 4.75 {
		t.Errorf("EPSHigh = %v, want 4.75", g.EPSHigh)
	}
	if g.Period.FiscalQuarter != "FY" {
		t.Errorf("period = %q, want FY", g.Period.FiscalQuarter)
	}
}

func TestExtract_RevenueGuidance(t *testing.T) {
	text := `We are lowering our fiscal 2026 revenue guidance to $12.5 billion to $13.0 billion,
reflecting softening demand in the enterprise segment.`
	g := Extract(text, "identity-002", "TEST")
	if !g.HasGuidance {
		t.Fatal("expected HasGuidance=true")
	}
	if g.Action != ActionLowered {
		t.Errorf("action = %q, want lowered", g.Action)
	}
	if g.Metric != MetricRevenue {
		t.Errorf("metric = %q, want revenue", g.Metric)
	}
	if g.RevenueLow == nil || *g.RevenueLow != 12500 {
		t.Errorf("RevenueLow = %v, want 12500 (millions)", g.RevenueLow)
	}
	if g.RevenueLow != nil && g.RevenueHigh != nil && *g.RevenueHigh != 13000 {
		t.Errorf("RevenueHigh = %v, want 13000 (millions)", g.RevenueHigh)
	}
	if g.Period.FiscalYear != 2026 {
		t.Errorf("fiscal year = %d, want 2026", g.Period.FiscalYear)
	}
}

func TestExtract_WithdrawnGuidance(t *testing.T) {
	text := `Due to macroeconomic uncertainty, the Company is withdrawing its full-year 2026 guidance
previously issued on March 15, 2026.`
	g := Extract(text, "identity-003", "TEST")
	if !g.HasGuidance {
		t.Fatal("expected HasGuidance=true for withdrawn guidance")
	}
	if g.Action != ActionWithdrawn {
		t.Errorf("action = %q, want withdrawn", g.Action)
	}
	if g.Confidence < 0.60 {
		t.Errorf("confidence = %.2f, want >= 0.60 for withdrawn guidance", g.Confidence)
	}
}

func TestExtract_NoGuidance(t *testing.T) {
	text := `ACME Corp reports record Q2 revenue of $5.2 billion. Net income was $1.1 billion.
Diluted EPS was $2.34, up 18% year over year.`
	g := Extract(text, "identity-004", "ACME")
	if g.HasGuidance {
		t.Error("expected HasGuidance=false for earnings-only press release")
	}
}

func TestGenerate_ProducesArticle(t *testing.T) {
	g := &GuidanceData{
		SourceIdentity: "src-001",
		Ticker:         "SCHW",
		Issuer:         "Charles Schwab",
		Action:         ActionRaised,
		Metric:         MetricEPS,
		Period:         Period{FiscalQuarter: "FY", FiscalYear: 2026},
		HasGuidance:    true,
		Confidence:     0.85,
	}
	lo, hi := 3.80, 3.95
	g.EPSLow = &lo
	g.EPSHigh = &hi

	art, ok := Generate(g, time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC))
	if !ok {
		t.Fatal("Generate returned false; expected article")
	}
	if art.Headline == "" {
		t.Error("empty headline")
	}
	if art.Ticker != "SCHW" {
		t.Errorf("ticker = %q, want SCHW", art.Ticker)
	}
	if art.Action != ActionRaised {
		t.Errorf("action = %q, want raised", art.Action)
	}
}

func TestGenerate_LowConfidenceRejected(t *testing.T) {
	g := &GuidanceData{
		HasGuidance: true,
		Confidence:  0.40,
		Ticker:      "TEST",
		Issuer:      "Test Co",
	}
	_, ok := Generate(g, time.Now())
	if ok {
		t.Error("expected Generate to reject low-confidence guidance")
	}
}
