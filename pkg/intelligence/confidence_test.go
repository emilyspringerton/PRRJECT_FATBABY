package intelligence_test

import (
	"testing"

	"github.com/example/prrject-fatbaby/pkg/intelligence"
)

func fullSignal(ticker, signalType string) intelligence.Signal {
	return intelligence.Signal{
		Ticker:         ticker,
		SignalType:     signalType,
		Summary:        "summary",
		ImpactAnalysis: "impact",
		Importance:     7,
	}
}

func TestScoreSignal_SecEpsBeat(t *testing.T) {
	q := intelligence.ScoreSignal(fullSignal("AAPL", "eps_beat"), "sec_8k")
	if q.Score < 0.80 {
		t.Errorf("institutional grade signal: want ≥0.80, got %.3f", q.Score)
	}
	if len(q.Flags) != 0 {
		t.Errorf("expected no flags, got %v", q.Flags)
	}
}

func TestScoreSignal_SourceTrustSEC(t *testing.T) {
	q := intelligence.ScoreSignal(fullSignal("MSFT", "guidance_raise"), "sec_8k")
	if q.SourceTrust != 0.90 {
		t.Errorf("want SourceTrust=0.90 for sec_8k, got %.2f", q.SourceTrust)
	}
}

func TestScoreSignal_SourceTrustPR(t *testing.T) {
	q := intelligence.ScoreSignal(fullSignal("MSFT", "product_launch"), "press_release")
	if q.SourceTrust != 0.75 {
		t.Errorf("want SourceTrust=0.75 for press_release, got %.2f", q.SourceTrust)
	}
}

func TestScoreSignal_SourceTrustUnknown(t *testing.T) {
	q := intelligence.ScoreSignal(fullSignal("X", "merger_acquisition"), "")
	if q.SourceTrust != 0.50 {
		t.Errorf("want SourceTrust=0.50 for unknown, got %.2f", q.SourceTrust)
	}
}

func TestScoreSignal_CompletenessFullFields(t *testing.T) {
	q := intelligence.ScoreSignal(fullSignal("AAPL", "eps_beat"), "sec_8k")
	if q.Completeness != 1.0 {
		t.Errorf("want 1.0 completeness for full signal, got %.2f", q.Completeness)
	}
}

func TestScoreSignal_CompletenessPartialFields(t *testing.T) {
	s := intelligence.Signal{Ticker: "AAPL", SignalType: "eps_beat"}
	q := intelligence.ScoreSignal(s, "sec_8k")
	if q.Completeness >= 1.0 {
		t.Errorf("expected < 1.0 completeness for partial signal, got %.2f", q.Completeness)
	}
	expected := false
	for _, f := range q.Flags {
		if f == "incomplete_extraction" {
			expected = true
		}
	}
	if !expected {
		t.Error("expected incomplete_extraction flag for partial signal")
	}
}

func TestScoreSignal_MissingTickerFlag(t *testing.T) {
	s := intelligence.Signal{SignalType: "eps_beat", Summary: "s", ImpactAnalysis: "i", Importance: 5}
	q := intelligence.ScoreSignal(s, "sec_8k")
	hasFlag := false
	for _, f := range q.Flags {
		if f == "missing_ticker" {
			hasFlag = true
		}
	}
	if !hasFlag {
		t.Error("expected missing_ticker flag")
	}
}

func TestScoreSignal_MissingSignalTypeFlag(t *testing.T) {
	s := intelligence.Signal{Ticker: "AAPL", Summary: "s", ImpactAnalysis: "i", Importance: 5}
	q := intelligence.ScoreSignal(s, "sec_8k")
	hasFlag := false
	for _, f := range q.Flags {
		if f == "missing_signal_type" {
			hasFlag = true
		}
	}
	if !hasFlag {
		t.Error("expected missing_signal_type flag")
	}
}

func TestScoreSignal_LowConfidenceFlag(t *testing.T) {
	s := intelligence.Signal{} // nothing filled
	q := intelligence.ScoreSignal(s, "")
	hasLow := false
	for _, f := range q.Flags {
		if f == "low_confidence" {
			hasLow = true
		}
	}
	if !hasLow {
		t.Error("expected low_confidence flag for empty signal")
	}
}

func TestScoreSignal_Clamped(t *testing.T) {
	q := intelligence.ScoreSignal(fullSignal("AAPL", "eps_beat"), "sec_8k")
	if q.Score > 1.0 {
		t.Errorf("score should be clamped to 1.0, got %.3f", q.Score)
	}
}

func TestScoreSignal_QuantBonusDividend(t *testing.T) {
	q := intelligence.ScoreSignal(fullSignal("T", "dividend_raise"), "press_release")
	if q.QuantitativeBonus != 0.90 {
		t.Errorf("want 0.90 quant bonus for dividend_raise, got %.2f", q.QuantitativeBonus)
	}
}

func TestScoreSignal_QuantBonusUnknownType(t *testing.T) {
	q := intelligence.ScoreSignal(fullSignal("X", ""), "sec_8k")
	if q.QuantitativeBonus != 0.50 {
		t.Errorf("want 0.50 quant bonus for unknown type, got %.2f", q.QuantitativeBonus)
	}
}

func TestGrade_A(t *testing.T) {
	if g := intelligence.Grade(0.90); g != "A" {
		t.Errorf("want A, got %s", g)
	}
}
func TestGrade_B(t *testing.T) {
	if g := intelligence.Grade(0.80); g != "B" {
		t.Errorf("want B, got %s", g)
	}
}
func TestGrade_C(t *testing.T) {
	if g := intelligence.Grade(0.70); g != "C" {
		t.Errorf("want C, got %s", g)
	}
}
func TestGrade_D(t *testing.T) {
	if g := intelligence.Grade(0.55); g != "D" {
		t.Errorf("want D, got %s", g)
	}
}
func TestGrade_F(t *testing.T) {
	if g := intelligence.Grade(0.30); g != "F" {
		t.Errorf("want F, got %s", g)
	}
}

func TestScoreSignal_InsiderBuy(t *testing.T) {
	q := intelligence.ScoreSignal(fullSignal("NVDA", "insider_buy"), "sec_form4")
	if q.QuantitativeBonus != 0.85 {
		t.Errorf("want 0.85 for insider_buy, got %.2f", q.QuantitativeBonus)
	}
}

func TestScoreSignal_NTFiling(t *testing.T) {
	q := intelligence.ScoreSignal(fullSignal("XYZ", "late_filing_nt10k"), "sec_nt10k")
	if q.SourceTrust != 0.80 {
		t.Errorf("want 0.80 source trust for NT10K, got %.2f", q.SourceTrust)
	}
}
