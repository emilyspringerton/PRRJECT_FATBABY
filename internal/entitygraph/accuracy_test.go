package entitygraph

import (
	"testing"
	"time"
)

func TestCorrelateActivistRisk_Confirmed(t *testing.T) {
	predicted := time.Now().UTC().AddDate(0, -3, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 9, 0).Format("2006-01-02")
	filedDate := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02") // 1 month ago, within window

	sigs := []Signal{{
		SignalID:     "activist_risk_schw_test",
		Ticker:       "SCHW",
		Type:         SignalActivistRisk,
		DetectedAt:   predicted,
		ValidThrough: validThru,
	}}
	filings := []Schd13Filing{{
		Ticker:     "SCHW",
		FilingDate: filedDate,
		FilingType: "SC 13D",
		FilerName:  "Test Activist LLC",
	}}

	records := CorrelateActivistRisk(sigs, filings)
	if len(records) != 1 {
		t.Fatalf("expected 1 accuracy record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed", records[0].Outcome)
	}
	if records[0].EvidenceDate != filedDate {
		t.Errorf("evidence_date = %s, want %s", records[0].EvidenceDate, filedDate)
	}
	if records[0].EvidenceType != "activist_13d" {
		t.Errorf("evidence_type = %s, want activist_13d", records[0].EvidenceType)
	}
}

func TestCorrelateActivistRisk_Refuted(t *testing.T) {
	// Signal was predicted last year, window already expired.
	predicted := time.Now().UTC().AddDate(-2, 0, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(-1, 0, 0).Format("2006-01-02")

	sigs := []Signal{{
		SignalID:     "activist_risk_old",
		Ticker:       "GS",
		Type:         SignalActivistRisk,
		DetectedAt:   predicted,
		ValidThrough: validThru,
	}}

	records := CorrelateActivistRisk(sigs, nil)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTRefuted {
		t.Errorf("outcome = %s, want refuted (window expired, no 13D)", records[0].Outcome)
	}
}

func TestCorrelateActivistRisk_Pending(t *testing.T) {
	today := time.Now().UTC().Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(1, 0, 0).Format("2006-01-02")

	sigs := []Signal{{
		SignalID:     "activist_risk_pending",
		Ticker:       "MS",
		Type:         SignalActivistRisk,
		DetectedAt:   today,
		ValidThrough: validThru,
	}}

	records := CorrelateActivistRisk(sigs, nil)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTPending {
		t.Errorf("outcome = %s, want pending (window open, no 13D yet)", records[0].Outcome)
	}
}

func TestCorrelateActivistRisk_SkipsNonActivistSignals(t *testing.T) {
	today := time.Now().UTC().Format("2006-01-02")
	sigs := []Signal{
		{SignalID: "friction_test", Ticker: "SCHW", Type: SignalDirectorFriction, DetectedAt: today, ValidThrough: today},
		{SignalID: "entrench_test", Ticker: "SCHW", Type: SignalGovernanceEntrenchment, DetectedAt: today, ValidThrough: today},
	}
	records := CorrelateActivistRisk(sigs, nil)
	if len(records) != 0 {
		t.Errorf("expected 0 records for non-activist signals, got %d", len(records))
	}
}

func TestCorrelateActivistRisk_FilingBeforeSignal_NotConfirmed(t *testing.T) {
	// 13D filed before the signal was generated — should not confirm.
	predicted := time.Now().UTC().Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(1, 0, 0).Format("2006-01-02")
	filedBefore := time.Now().UTC().AddDate(0, -6, 0).Format("2006-01-02")

	sigs := []Signal{{
		SignalID:     "activist_risk_new",
		Ticker:       "C",
		Type:         SignalActivistRisk,
		DetectedAt:   predicted,
		ValidThrough: validThru,
	}}
	filings := []Schd13Filing{{
		Ticker:     "C",
		FilingDate: filedBefore, // before signal
		FilingType: "SC 13D",
	}}

	records := CorrelateActivistRisk(sigs, filings)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	// Filing was before the signal — should be pending, not confirmed.
	if records[0].Outcome == GTConfirmed {
		t.Error("filing before signal should not confirm the prediction")
	}
}

func TestCorrelateActivistRisk_13G_DoesNotConfirm(t *testing.T) {
	// SC 13G is a passive holder filing — not an activist; should not confirm activist_risk.
	today := time.Now().UTC().Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(1, 0, 0).Format("2006-01-02")

	sigs := []Signal{{
		SignalID:     "activist_risk_ibkr",
		Ticker:       "IBKR",
		Type:         SignalActivistRisk,
		DetectedAt:   today,
		ValidThrough: validThru,
	}}
	filings := []Schd13Filing{{
		Ticker:     "IBKR",
		FilingDate: today,
		FilingType: "SC 13G", // passive — should NOT confirm activist prediction
	}}

	records := CorrelateActivistRisk(sigs, filings)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome == GTConfirmed {
		t.Error("SC 13G (passive holder) should not confirm activist_risk prediction")
	}
}

func TestBuildAccuracyReports_Precision(t *testing.T) {
	records := []AccuracyRecord{
		{SignalType: SignalActivistRisk, Outcome: GTConfirmed},
		{SignalType: SignalActivistRisk, Outcome: GTConfirmed},
		{SignalType: SignalActivistRisk, Outcome: GTRefuted},
		{SignalType: SignalActivistRisk, Outcome: GTPending},
	}
	reports := BuildAccuracyReports(records)
	if len(reports) != 1 {
		t.Fatalf("expected 1 report, got %d", len(reports))
	}
	r := reports[0]
	if r.TotalPredictions != 4 {
		t.Errorf("total = %d, want 4", r.TotalPredictions)
	}
	if r.Confirmed != 2 {
		t.Errorf("confirmed = %d, want 2", r.Confirmed)
	}
	if r.Refuted != 1 {
		t.Errorf("refuted = %d, want 1", r.Refuted)
	}
	if r.Pending != 1 {
		t.Errorf("pending = %d, want 1", r.Pending)
	}
	// precision = 2/(2+1) = 0.667
	if r.Precision < 0.66 || r.Precision > 0.68 {
		t.Errorf("precision = %.3f, want ~0.667", r.Precision)
	}
}

func TestBuildAccuracyReports_NoPrecisionWithOnlyPending(t *testing.T) {
	records := []AccuracyRecord{
		{SignalType: SignalActivistRisk, Outcome: GTPending},
		{SignalType: SignalActivistRisk, Outcome: GTPending},
	}
	reports := BuildAccuracyReports(records)
	if len(reports) != 1 {
		t.Fatalf("expected 1 report, got %d", len(reports))
	}
	if reports[0].Precision != 0.0 {
		t.Errorf("precision should be 0 when no resolved predictions, got %.3f", reports[0].Precision)
	}
}

// ── Health history tests ──────────────────────────────────────────────────────

func TestAppendAndLoadHealthHistory(t *testing.T) {
	dir := t.TempDir()
	snapshots := []HealthSnapshot{
		{Ticker: "SCHW", Score: 0.42, RecordedAt: "2026-06-01"},
		{Ticker: "AAPL", Score: 0.88, RecordedAt: "2026-06-01"},
	}
	if err := AppendHealthSnapshot(dir, snapshots); err != nil {
		t.Fatalf("append: %v", err)
	}
	hist, err := LoadHealthHistory(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(hist) != 2 {
		t.Fatalf("want 2 tickers, got %d", len(hist))
	}
	if hist["SCHW"].Score != 0.42 {
		t.Errorf("SCHW score: want 0.42, got %.3f", hist["SCHW"].Score)
	}
	if hist["AAPL"].Score != 0.88 {
		t.Errorf("AAPL score: want 0.88, got %.3f", hist["AAPL"].Score)
	}
}

func TestLoadHealthHistory_MissingFile(t *testing.T) {
	dir := t.TempDir()
	hist, err := LoadHealthHistory(dir)
	if err != nil {
		t.Fatalf("unexpected error for missing file: %v", err)
	}
	if len(hist) != 0 {
		t.Errorf("want empty map, got %d entries", len(hist))
	}
}

func TestLoadHealthHistory_LastWriteWins(t *testing.T) {
	dir := t.TempDir()
	// Write old score then update.
	AppendHealthSnapshot(dir, []HealthSnapshot{{Ticker: "SCHW", Score: 0.50, RecordedAt: "2026-05-01"}})
	AppendHealthSnapshot(dir, []HealthSnapshot{{Ticker: "SCHW", Score: 0.35, RecordedAt: "2026-06-01"}})
	hist, _ := LoadHealthHistory(dir)
	if hist["SCHW"].Score != 0.35 {
		t.Errorf("last-write-wins: want 0.35, got %.3f", hist["SCHW"].Score)
	}
}
