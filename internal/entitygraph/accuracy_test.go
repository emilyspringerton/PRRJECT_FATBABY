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

// ── CorrelateDecayDeparture tests ─────────────────────────────────────────────

func TestCorrelateDecayDeparture_Confirmed(t *testing.T) {
	decayAt := time.Now().UTC().AddDate(0, -6, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 6, 0).Format("2006-01-02")
	departDate := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")

	sigs := []Signal{
		{
			SignalID:     "decay_herringer_schw",
			Type:         SignalDirectorDecay,
			Ticker:       "SCHW",
			Entity:       "Frank Herringer",
			DetectedAt:   decayAt,
			ValidThrough: validThru,
		},
		{
			Type:       SignalLeadershipDeparture,
			Ticker:     "SCHW",
			Entity:     "Frank Herringer",
			FilingDate: departDate,
			DetectedAt: departDate,
		},
	}
	records := CorrelateDecayDeparture(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 accuracy record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed", records[0].Outcome)
	}
	if records[0].EvidenceType != "leadership_departure" {
		t.Errorf("evidence_type = %s, want leadership_departure", records[0].EvidenceType)
	}
}

func TestCorrelateDecayDeparture_Refuted(t *testing.T) {
	decayAt := time.Now().UTC().AddDate(-2, 0, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(-1, 0, 0).Format("2006-01-02")

	sigs := []Signal{{
		SignalID:     "decay_old",
		Type:         SignalDirectorDecay,
		Ticker:       "GS",
		Entity:       "Jane Smith",
		DetectedAt:   decayAt,
		ValidThrough: validThru,
	}}
	records := CorrelateDecayDeparture(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTRefuted {
		t.Errorf("outcome = %s, want refuted (window expired, no departure)", records[0].Outcome)
	}
}

func TestCorrelateDecayDeparture_Pending(t *testing.T) {
	today := time.Now().UTC().Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(1, 0, 0).Format("2006-01-02")

	sigs := []Signal{{
		SignalID:     "decay_pending",
		Type:         SignalDirectorDecay,
		Ticker:       "MS",
		Entity:       "Bob Jones",
		DetectedAt:   today,
		ValidThrough: validThru,
	}}
	records := CorrelateDecayDeparture(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTPending {
		t.Errorf("outcome = %s, want pending (window open, no departure yet)", records[0].Outcome)
	}
}

func TestCorrelateDecayDeparture_SkipsNonDecaySignals(t *testing.T) {
	today := time.Now().UTC().Format("2006-01-02")
	sigs := []Signal{
		{Type: SignalDirectorFriction, Ticker: "AAPL", DetectedAt: today, ValidThrough: today},
		{Type: SignalActivistRisk, Ticker: "AAPL", DetectedAt: today, ValidThrough: today},
	}
	records := CorrelateDecayDeparture(sigs)
	if len(records) != 0 {
		t.Errorf("expected 0 records for non-decay signals, got %d", len(records))
	}
}

func TestCorrelateDecayDeparture_DepartureBeforeDecay_NotConfirmed(t *testing.T) {
	decayAt := time.Now().UTC().Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(1, 0, 0).Format("2006-01-02")
	departBefore := time.Now().UTC().AddDate(0, -3, 0).Format("2006-01-02")

	sigs := []Signal{
		{
			SignalID:     "decay_new",
			Type:         SignalDirectorDecay,
			Ticker:       "C",
			Entity:       "Alice Chen",
			DetectedAt:   decayAt,
			ValidThrough: validThru,
		},
		{
			Type:       SignalLeadershipDeparture,
			Ticker:     "C",
			Entity:     "Alice Chen",
			FilingDate: departBefore, // before the decay signal
			DetectedAt: departBefore,
		},
	}
	records := CorrelateDecayDeparture(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome == GTConfirmed {
		t.Error("departure before decay signal should not confirm prediction")
	}
}

func TestCorrelateDecayDeparture_SubstringEntityMatch(t *testing.T) {
	decayAt := time.Now().UTC().AddDate(0, -3, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 9, 0).Format("2006-01-02")
	departDate := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	// Decay uses canonical "herringer"; departure uses display "Frank C. Herringer" — should still match.
	sigs := []Signal{
		{
			SignalID:     "decay_herringer",
			Type:         SignalDirectorDecay,
			Ticker:       "SCHW",
			Entity:       "herringer",
			DetectedAt:   decayAt,
			ValidThrough: validThru,
		},
		{
			Type:       SignalLeadershipDeparture,
			Ticker:     "SCHW",
			Entity:     "Frank C. Herringer",
			FilingDate: departDate,
			DetectedAt: departDate,
		},
	}
	records := CorrelateDecayDeparture(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed (substring entity match)", records[0].Outcome)
	}
}

// ── CorrelateAuditorChangeFilingRisk ─────────────────────────────────────────

func TestCorrelateAuditorChangeFilingRisk_ConfirmedByLateFiling(t *testing.T) {
	auditorAt := time.Now().UTC().AddDate(0, -4, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 8, 0).Format("2006-01-02")
	lateAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{
			SignalID: "auditor_change_schw_test", Type: SignalAuditorChange, Ticker: "SCHW",
			DetectedAt: auditorAt, ValidThrough: validThru, FilingDate: auditorAt,
		},
		{
			Type: SignalLateFiling, Ticker: "SCHW",
			FilingDate: lateAt, DetectedAt: lateAt,
		},
	}
	records := CorrelateAuditorChangeFilingRisk(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed", records[0].Outcome)
	}
	if records[0].EvidenceType != "late_filing_or_eps_revision" {
		t.Errorf("evidence_type = %s, want late_filing_or_eps_revision", records[0].EvidenceType)
	}
}

func TestCorrelateAuditorChangeFilingRisk_ConfirmedByEPSRevision(t *testing.T) {
	auditorAt := time.Now().UTC().AddDate(0, -4, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 8, 0).Format("2006-01-02")
	revAt := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")

	sigs := []Signal{
		{
			SignalID: "auditor_change_mmm_test", Type: SignalAuditorChange, Ticker: "MMM",
			DetectedAt: auditorAt, ValidThrough: validThru,
		},
		{
			Type: SignalEPSFilingRevision, Ticker: "MMM",
			DetectedAt: revAt,
		},
	}
	records := CorrelateAuditorChangeFilingRisk(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed", records[0].Outcome)
	}
}

func TestCorrelateAuditorChangeFilingRisk_Refuted(t *testing.T) {
	auditorAt := time.Now().UTC().AddDate(0, -14, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02") // expired

	sigs := []Signal{{
		SignalID: "auditor_change_expired", Type: SignalAuditorChange, Ticker: "XYZ",
		DetectedAt: auditorAt, ValidThrough: validThru,
	}}
	records := CorrelateAuditorChangeFilingRisk(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTRefuted {
		t.Errorf("outcome = %s, want refuted (window expired)", records[0].Outcome)
	}
}

func TestCorrelateAuditorChangeFilingRisk_Pending(t *testing.T) {
	auditorAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 11, 0).Format("2006-01-02")

	sigs := []Signal{{
		SignalID: "auditor_change_pending", Type: SignalAuditorChange, Ticker: "AAPL",
		DetectedAt: auditorAt, ValidThrough: validThru,
	}}
	records := CorrelateAuditorChangeFilingRisk(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTPending {
		t.Errorf("outcome = %s, want pending", records[0].Outcome)
	}
}

func TestCorrelateAuditorChangeFilingRisk_WrongTickerIgnored(t *testing.T) {
	auditorAt := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 10, 0).Format("2006-01-02")
	lateAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{
			SignalID: "auditor_change_foo", Type: SignalAuditorChange, Ticker: "FOO",
			DetectedAt: auditorAt, ValidThrough: validThru,
		},
		{
			Type: SignalLateFiling, Ticker: "BAR", // different ticker
			FilingDate: lateAt, DetectedAt: lateAt,
		},
	}
	records := CorrelateAuditorChangeFilingRisk(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTPending {
		t.Errorf("outcome = %s, want pending (wrong ticker late filing should not confirm)", records[0].Outcome)
	}
}

func TestCorrelateAuditorChangeFilingRisk_EventBeforeWindow(t *testing.T) {
	auditorAt := time.Now().UTC().AddDate(0, -3, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 9, 0).Format("2006-01-02")
	lateAt := time.Now().UTC().AddDate(0, -4, 0).Format("2006-01-02") // before auditor change

	sigs := []Signal{
		{
			SignalID: "auditor_change_late_before", Type: SignalAuditorChange, Ticker: "MSFT",
			DetectedAt: auditorAt, ValidThrough: validThru,
		},
		{
			Type: SignalLateFiling, Ticker: "MSFT",
			FilingDate: lateAt, DetectedAt: lateAt,
		},
	}
	records := CorrelateAuditorChangeFilingRisk(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTPending {
		t.Errorf("outcome = %s, want pending (event before window start should not confirm)", records[0].Outcome)
	}
}

// ── CorrelateInsiderBuyCapitalReturn ─────────────────────────────────────────

func TestCorrelateInsiderBuyCapitalReturn_ConfirmedByBuyback(t *testing.T) {
	buyAt := time.Now().UTC().AddDate(0, -3, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 9, 0).Format("2006-01-02")
	buybackAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "insider_buy_test", Type: SignalInsiderBuy, Ticker: "SCHW",
			DetectedAt: buyAt, ValidThrough: validThru},
		{Type: SignalBuybackAuthorization, Ticker: "SCHW",
			DetectedAt: buybackAt, FilingDate: buybackAt},
	}
	records := CorrelateInsiderBuyCapitalReturn(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed", records[0].Outcome)
	}
	if records[0].EvidenceType != "buyback_or_dividend_raise" {
		t.Errorf("evidence_type = %s, want buyback_or_dividend_raise", records[0].EvidenceType)
	}
}

func TestCorrelateInsiderBuyCapitalReturn_ConfirmedByDividendRaise(t *testing.T) {
	buyAt := time.Now().UTC().AddDate(0, -4, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 8, 0).Format("2006-01-02")
	raiseAt := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "insider_buy_div_test", Type: SignalInsiderBuy, Ticker: "JPM",
			DetectedAt: buyAt, ValidThrough: validThru},
		{Type: SignalDividendRaise, Ticker: "JPM",
			DetectedAt: raiseAt, FilingDate: raiseAt},
	}
	records := CorrelateInsiderBuyCapitalReturn(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed", records[0].Outcome)
	}
}

func TestCorrelateInsiderBuyCapitalReturn_Refuted(t *testing.T) {
	buyAt := time.Now().UTC().AddDate(0, -14, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")

	sigs := []Signal{{SignalID: "insider_buy_expired", Type: SignalInsiderBuy, Ticker: "XYZ",
		DetectedAt: buyAt, ValidThrough: validThru}}
	records := CorrelateInsiderBuyCapitalReturn(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTRefuted {
		t.Errorf("outcome = %s, want refuted", records[0].Outcome)
	}
}

func TestCorrelateInsiderBuyCapitalReturn_WrongTickerIgnored(t *testing.T) {
	buyAt := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 10, 0).Format("2006-01-02")
	buybackAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "insider_buy_wrong_ticker", Type: SignalInsiderBuy, Ticker: "AAA",
			DetectedAt: buyAt, ValidThrough: validThru},
		{Type: SignalBuybackAuthorization, Ticker: "BBB", DetectedAt: buybackAt, FilingDate: buybackAt},
	}
	records := CorrelateInsiderBuyCapitalReturn(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTPending {
		t.Errorf("outcome = %s, want pending (wrong ticker)", records[0].Outcome)
	}
}

// ── CorrelateInsiderSellDistress ─────────────────────────────────────────────

func TestCorrelateInsiderSellDistress_ConfirmedByDividendCut(t *testing.T) {
	sellAt := time.Now().UTC().AddDate(0, -3, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 9, 0).Format("2006-01-02")
	cutAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "insider_sell_cluster_test", Type: SignalInsiderSellCluster, Ticker: "GE",
			DetectedAt: sellAt, ValidThrough: validThru},
		{Type: SignalDividendCut, Ticker: "GE", DetectedAt: cutAt, FilingDate: cutAt},
	}
	records := CorrelateInsiderSellDistress(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed", records[0].Outcome)
	}
	if records[0].EvidenceType != "dividend_cut_cfo_departure_or_late_filing" {
		t.Errorf("evidence_type = %s, want dividend_cut_cfo_departure_or_late_filing", records[0].EvidenceType)
	}
}

func TestCorrelateInsiderSellDistress_ConfirmedByCFODeparture(t *testing.T) {
	sellAt := time.Now().UTC().AddDate(0, -4, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 8, 0).Format("2006-01-02")
	deptAt := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "insider_sell_cfo_test", Type: SignalInsiderSellCluster, Ticker: "WFC",
			DetectedAt: sellAt, ValidThrough: validThru},
		{Type: SignalCFODeparture, Ticker: "WFC", DetectedAt: deptAt, FilingDate: deptAt},
	}
	records := CorrelateInsiderSellDistress(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed", records[0].Outcome)
	}
}

func TestCorrelateInsiderSellDistress_Refuted(t *testing.T) {
	sellAt := time.Now().UTC().AddDate(0, -15, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, -3, 0).Format("2006-01-02")

	sigs := []Signal{{SignalID: "insider_sell_refuted", Type: SignalInsiderSellCluster, Ticker: "ABC",
		DetectedAt: sellAt, ValidThrough: validThru}}
	records := CorrelateInsiderSellDistress(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTRefuted {
		t.Errorf("outcome = %s, want refuted", records[0].Outcome)
	}
}

func TestCorrelateInsiderSellDistress_ConfirmedByLateFiling(t *testing.T) {
	sellAt := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 10, 0).Format("2006-01-02")
	lateAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "insider_sell_late_test", Type: SignalInsiderSellCluster, Ticker: "MMM",
			DetectedAt: sellAt, ValidThrough: validThru},
		{Type: SignalLateFiling, Ticker: "MMM", DetectedAt: lateAt, FilingDate: lateAt},
	}
	records := CorrelateInsiderSellDistress(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed (late filing = distress)", records[0].Outcome)
	}
}

// ── CorrelateCFODepartureDistress ─────────────────────────────────────────────

func TestCorrelateCFODepartureDistress_ConfirmedByDividendCut(t *testing.T) {
	cfoAt := time.Now().UTC().AddDate(0, -3, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 9, 0).Format("2006-01-02")
	cutAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "cfo_dep_test", Type: SignalCFODeparture, Ticker: "XOM",
			DetectedAt: cfoAt, ValidThrough: validThru},
		{Type: SignalDividendCut, Ticker: "XOM", DetectedAt: cutAt},
	}
	records := CorrelateCFODepartureDistress(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed", records[0].Outcome)
	}
}

func TestCorrelateCFODepartureDistress_ConfirmedByLateFiling(t *testing.T) {
	cfoAt := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 10, 0).Format("2006-01-02")
	lateAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "cfo_dep_late_test", Type: SignalCFODeparture, Ticker: "MRK",
			DetectedAt: cfoAt, ValidThrough: validThru},
		{Type: SignalLateFiling, Ticker: "MRK", DetectedAt: lateAt},
	}
	records := CorrelateCFODepartureDistress(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed", records[0].Outcome)
	}
}

func TestCorrelateCFODepartureDistress_Refuted(t *testing.T) {
	cfoAt := time.Now().UTC().AddDate(0, -13, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "cfo_dep_refuted", Type: SignalCFODeparture, Ticker: "CAT",
			DetectedAt: cfoAt, ValidThrough: validThru},
	}
	records := CorrelateCFODepartureDistress(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTRefuted {
		t.Errorf("outcome = %s, want refuted (window expired)", records[0].Outcome)
	}
}

func TestCorrelateCFODepartureDistress_WrongTickerIgnored(t *testing.T) {
	cfoAt := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 10, 0).Format("2006-01-02")
	cutAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "cfo_dep_wrong", Type: SignalCFODeparture, Ticker: "AAPL",
			DetectedAt: cfoAt, ValidThrough: validThru},
		{Type: SignalDividendCut, Ticker: "MSFT", DetectedAt: cutAt},
	}
	records := CorrelateCFODepartureDistress(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTPending {
		t.Errorf("outcome = %s, want pending (different ticker)", records[0].Outcome)
	}
}

// ── CorrelateDirectorFrictionEscalation ──────────────────────────────────────

func TestCorrelateDirectorFrictionEscalation_ConfirmedByCompensationConcern(t *testing.T) {
	frictionAt := time.Now().UTC().AddDate(0, -4, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 8, 0).Format("2006-01-02")
	compAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "dir_fric_comp_test", Type: SignalDirectorFriction, Ticker: "GS",
			DetectedAt: frictionAt, ValidThrough: validThru},
		{Type: SignalCompensationConcern, Ticker: "GS", DetectedAt: compAt},
	}
	records := CorrelateDirectorFrictionEscalation(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed", records[0].Outcome)
	}
}

func TestCorrelateDirectorFrictionEscalation_ConfirmedByAbstentionSpike(t *testing.T) {
	frictionAt := time.Now().UTC().AddDate(0, -3, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 9, 0).Format("2006-01-02")
	abstAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "dir_fric_abst_test", Type: SignalDirectorFriction, Ticker: "JPM",
			DetectedAt: frictionAt, ValidThrough: validThru},
		{Type: SignalAbstentionSpike, Ticker: "JPM", DetectedAt: abstAt},
	}
	records := CorrelateDirectorFrictionEscalation(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed", records[0].Outcome)
	}
}

func TestCorrelateDirectorFrictionEscalation_ConfirmedByNominationRejection(t *testing.T) {
	frictionAt := time.Now().UTC().AddDate(0, -3, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 9, 0).Format("2006-01-02")
	nomAt := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "dir_fric_nom_test", Type: SignalDirectorFriction, Ticker: "WFC",
			DetectedAt: frictionAt, ValidThrough: validThru},
		{Type: SignalNominationRejection, Ticker: "WFC", DetectedAt: nomAt},
	}
	records := CorrelateDirectorFrictionEscalation(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed", records[0].Outcome)
	}
}

func TestCorrelateDirectorFrictionEscalation_Refuted(t *testing.T) {
	frictionAt := time.Now().UTC().AddDate(0, -14, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "dir_fric_refuted", Type: SignalDirectorFriction, Ticker: "BAC",
			DetectedAt: frictionAt, ValidThrough: validThru},
	}
	records := CorrelateDirectorFrictionEscalation(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTRefuted {
		t.Errorf("outcome = %s, want refuted", records[0].Outcome)
	}
}

func TestCorrelateDirectorFrictionEscalation_WrongTickerIgnored(t *testing.T) {
	frictionAt := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 10, 0).Format("2006-01-02")
	compAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "dir_fric_wrong", Type: SignalDirectorFriction, Ticker: "IBM",
			DetectedAt: frictionAt, ValidThrough: validThru},
		{Type: SignalCompensationConcern, Ticker: "ORCL", DetectedAt: compAt},
	}
	records := CorrelateDirectorFrictionEscalation(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTPending {
		t.Errorf("outcome = %s, want pending (different ticker)", records[0].Outcome)
	}
}

// ── CorrelateDividendCutDeterioration ─────────────────────────────────────────

func TestCorrelateDividendCutDeterioration_ConfirmedByCFODeparture(t *testing.T) {
	cutAt := time.Now().UTC().AddDate(0, -3, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 9, 0).Format("2006-01-02")
	cfoAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "divcut_cfo_test", Type: SignalDividendCut, Ticker: "GE",
			DetectedAt: cutAt, ValidThrough: validThru},
		{Type: SignalCFODeparture, Ticker: "GE", DetectedAt: cfoAt},
	}
	records := CorrelateDividendCutDeterioration(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed", records[0].Outcome)
	}
}

func TestCorrelateDividendCutDeterioration_ConfirmedByLateFiling(t *testing.T) {
	cutAt := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 10, 0).Format("2006-01-02")
	lateAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "divcut_late_test", Type: SignalDividendCut, Ticker: "F",
			DetectedAt: cutAt, ValidThrough: validThru},
		{Type: SignalLateFiling, Ticker: "F", DetectedAt: lateAt},
	}
	records := CorrelateDividendCutDeterioration(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed", records[0].Outcome)
	}
}

func TestCorrelateDividendCutDeterioration_Refuted(t *testing.T) {
	cutAt := time.Now().UTC().AddDate(0, -14, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "divcut_refuted", Type: SignalDividendCut, Ticker: "T",
			DetectedAt: cutAt, ValidThrough: validThru},
	}
	records := CorrelateDividendCutDeterioration(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTRefuted {
		t.Errorf("outcome = %s, want refuted", records[0].Outcome)
	}
}

func TestCorrelateDividendCutDeterioration_WrongTickerIgnored(t *testing.T) {
	cutAt := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 10, 0).Format("2006-01-02")
	cfoAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "divcut_wrong", Type: SignalDividendCut, Ticker: "VZ",
			DetectedAt: cutAt, ValidThrough: validThru},
		{Type: SignalCFODeparture, Ticker: "T", DetectedAt: cfoAt},
	}
	records := CorrelateDividendCutDeterioration(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTPending {
		t.Errorf("outcome = %s, want pending (wrong ticker)", records[0].Outcome)
	}
}

// ── CorrelateLateFilingDistress ───────────────────────────────────────────────

func TestCorrelateLateFilingDistress_ConfirmedByDividendCut(t *testing.T) {
	lateAt := time.Now().UTC().AddDate(0, -3, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 9, 0).Format("2006-01-02")
	cutAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "late_divcut_test", Type: SignalLateFiling, Ticker: "CVS",
			DetectedAt: lateAt, ValidThrough: validThru},
		{Type: SignalDividendCut, Ticker: "CVS", DetectedAt: cutAt},
	}
	records := CorrelateLateFilingDistress(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed", records[0].Outcome)
	}
}

func TestCorrelateLateFilingDistress_ConfirmedByCFODeparture(t *testing.T) {
	lateAt := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 10, 0).Format("2006-01-02")
	cfoAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "late_cfo_test", Type: SignalLateFiling, Ticker: "WBA",
			DetectedAt: lateAt, ValidThrough: validThru},
		{Type: SignalCFODeparture, Ticker: "WBA", DetectedAt: cfoAt},
	}
	records := CorrelateLateFilingDistress(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed", records[0].Outcome)
	}
}

func TestCorrelateLateFilingDistress_Refuted(t *testing.T) {
	lateAt := time.Now().UTC().AddDate(0, -13, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "late_refuted", Type: SignalLateFiling, Ticker: "KHC",
			DetectedAt: lateAt, ValidThrough: validThru},
	}
	records := CorrelateLateFilingDistress(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTRefuted {
		t.Errorf("outcome = %s, want refuted", records[0].Outcome)
	}
}

func TestCorrelateLateFilingDistress_WrongTickerIgnored(t *testing.T) {
	lateAt := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 10, 0).Format("2006-01-02")
	cutAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "late_wrong", Type: SignalLateFiling, Ticker: "M",
			DetectedAt: lateAt, ValidThrough: validThru},
		{Type: SignalDividendCut, Ticker: "JWN", DetectedAt: cutAt},
	}
	records := CorrelateLateFilingDistress(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTPending {
		t.Errorf("outcome = %s, want pending (wrong ticker)", records[0].Outcome)
	}
}

// ── CorrelateLeadershipDepartureDistress ──────────────────────────────────────

func TestCorrelateLeadershipDepartureDistress_ConfirmedByDividendCut(t *testing.T) {
	depAt := time.Now().UTC().AddDate(0, -3, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 9, 0).Format("2006-01-02")
	cutAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "lead_dep_test", Type: SignalLeadershipDeparture, Ticker: "DIS",
			DetectedAt: depAt, ValidThrough: validThru},
		{Type: SignalDividendCut, Ticker: "DIS", DetectedAt: cutAt},
	}
	records := CorrelateLeadershipDepartureDistress(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed", records[0].Outcome)
	}
}

func TestCorrelateLeadershipDepartureDistress_ConfirmedByCFODeparture(t *testing.T) {
	depAt := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 10, 0).Format("2006-01-02")
	cfoAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "lead_dep_cfo_test", Type: SignalLeadershipDeparture, Ticker: "INTC",
			DetectedAt: depAt, ValidThrough: validThru},
		{Type: SignalCFODeparture, Ticker: "INTC", DetectedAt: cfoAt},
	}
	records := CorrelateLeadershipDepartureDistress(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed", records[0].Outcome)
	}
}

func TestCorrelateLeadershipDepartureDistress_Refuted(t *testing.T) {
	depAt := time.Now().UTC().AddDate(0, -14, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "lead_dep_refuted", Type: SignalLeadershipDeparture, Ticker: "NFLX",
			DetectedAt: depAt, ValidThrough: validThru},
	}
	records := CorrelateLeadershipDepartureDistress(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTRefuted {
		t.Errorf("outcome = %s, want refuted", records[0].Outcome)
	}
}

func TestCorrelateLeadershipDepartureDistress_WrongTickerIgnored(t *testing.T) {
	depAt := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 10, 0).Format("2006-01-02")
	cutAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "lead_dep_wrong", Type: SignalLeadershipDeparture, Ticker: "META",
			DetectedAt: depAt, ValidThrough: validThru},
		{Type: SignalDividendCut, Ticker: "SNAP", DetectedAt: cutAt},
	}
	records := CorrelateLeadershipDepartureDistress(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTPending {
		t.Errorf("outcome = %s, want pending (wrong ticker)", records[0].Outcome)
	}
}

// ── CorrelateBuybackSuspensionDistress ────────────────────────────────────────

func TestCorrelateBuybackSuspensionDistress_ConfirmedByDividendCut(t *testing.T) {
	suspAt := time.Now().UTC().AddDate(0, -3, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 9, 0).Format("2006-01-02")
	cutAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "bb_susp_cut_test", Type: SignalBuybackSuspension, Ticker: "BA",
			DetectedAt: suspAt, ValidThrough: validThru},
		{Type: SignalDividendCut, Ticker: "BA", DetectedAt: cutAt},
	}
	records := CorrelateBuybackSuspensionDistress(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed", records[0].Outcome)
	}
}

func TestCorrelateBuybackSuspensionDistress_ConfirmedByLateFiling(t *testing.T) {
	suspAt := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 10, 0).Format("2006-01-02")
	lateAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "bb_susp_late_test", Type: SignalBuybackSuspension, Ticker: "GM",
			DetectedAt: suspAt, ValidThrough: validThru},
		{Type: SignalLateFiling, Ticker: "GM", DetectedAt: lateAt},
	}
	records := CorrelateBuybackSuspensionDistress(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed", records[0].Outcome)
	}
}

func TestCorrelateBuybackSuspensionDistress_Refuted(t *testing.T) {
	suspAt := time.Now().UTC().AddDate(0, -13, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "bb_susp_refuted", Type: SignalBuybackSuspension, Ticker: "FDX",
			DetectedAt: suspAt, ValidThrough: validThru},
	}
	records := CorrelateBuybackSuspensionDistress(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTRefuted {
		t.Errorf("outcome = %s, want refuted", records[0].Outcome)
	}
}

func TestCorrelateBuybackSuspensionDistress_WrongTickerIgnored(t *testing.T) {
	suspAt := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 10, 0).Format("2006-01-02")
	cutAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "bb_susp_wrong", Type: SignalBuybackSuspension, Ticker: "UPS",
			DetectedAt: suspAt, ValidThrough: validThru},
		{Type: SignalDividendCut, Ticker: "FDX", DetectedAt: cutAt},
	}
	records := CorrelateBuybackSuspensionDistress(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTPending {
		t.Errorf("outcome = %s, want pending (wrong ticker)", records[0].Outcome)
	}
}

// ── CorrelateAbstentionSpikeEscalation ───────────────────────────────────────

func TestCorrelateAbstentionSpikeEscalation_ConfirmedByNominationRejection(t *testing.T) {
	spikeAt := time.Now().UTC().AddDate(0, -4, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 8, 0).Format("2006-01-02")
	nomAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "abst_spike_nom_test", Type: SignalAbstentionSpike, Ticker: "CRM",
			DetectedAt: spikeAt, ValidThrough: validThru},
		{Type: SignalNominationRejection, Ticker: "CRM", DetectedAt: nomAt},
	}
	records := CorrelateAbstentionSpikeEscalation(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed", records[0].Outcome)
	}
}

func TestCorrelateAbstentionSpikeEscalation_ConfirmedByDirectorFriction(t *testing.T) {
	spikeAt := time.Now().UTC().AddDate(0, -3, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 9, 0).Format("2006-01-02")
	fricAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "abst_spike_fric_test", Type: SignalAbstentionSpike, Ticker: "AMZN",
			DetectedAt: spikeAt, ValidThrough: validThru},
		{Type: SignalDirectorFriction, Ticker: "AMZN", DetectedAt: fricAt},
	}
	records := CorrelateAbstentionSpikeEscalation(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed", records[0].Outcome)
	}
}

func TestCorrelateAbstentionSpikeEscalation_Refuted(t *testing.T) {
	spikeAt := time.Now().UTC().AddDate(0, -14, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "abst_spike_refuted", Type: SignalAbstentionSpike, Ticker: "TSLA",
			DetectedAt: spikeAt, ValidThrough: validThru},
	}
	records := CorrelateAbstentionSpikeEscalation(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTRefuted {
		t.Errorf("outcome = %s, want refuted", records[0].Outcome)
	}
}

func TestCorrelateAbstentionSpikeEscalation_WrongTickerIgnored(t *testing.T) {
	spikeAt := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 10, 0).Format("2006-01-02")
	nomAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "abst_spike_wrong", Type: SignalAbstentionSpike, Ticker: "NVDA",
			DetectedAt: spikeAt, ValidThrough: validThru},
		{Type: SignalNominationRejection, Ticker: "AMD", DetectedAt: nomAt},
	}
	records := CorrelateAbstentionSpikeEscalation(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTPending {
		t.Errorf("outcome = %s, want pending (wrong ticker)", records[0].Outcome)
	}
}

// ── CorrelateBoardDecayConcernDeterioration ───────────────────────────────────

func TestCorrelateBoardDecayConcernDeterioration_ConfirmedByDirectorFriction(t *testing.T) {
	decayAt := time.Now().UTC().AddDate(0, -4, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 8, 0).Format("2006-01-02")
	fricAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "board_decay_fric_test", Type: SignalBoardDecayConcern, Ticker: "WMT",
			DetectedAt: decayAt, ValidThrough: validThru},
		{Type: SignalDirectorFriction, Ticker: "WMT", DetectedAt: fricAt},
	}
	records := CorrelateBoardDecayConcernDeterioration(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed", records[0].Outcome)
	}
}

func TestCorrelateBoardDecayConcernDeterioration_ConfirmedByCFODeparture(t *testing.T) {
	decayAt := time.Now().UTC().AddDate(0, -3, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 9, 0).Format("2006-01-02")
	cfoAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "board_decay_cfo_test", Type: SignalBoardDecayConcern, Ticker: "TGT",
			DetectedAt: decayAt, ValidThrough: validThru},
		{Type: SignalCFODeparture, Ticker: "TGT", DetectedAt: cfoAt},
	}
	records := CorrelateBoardDecayConcernDeterioration(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed", records[0].Outcome)
	}
}

func TestCorrelateBoardDecayConcernDeterioration_Refuted(t *testing.T) {
	decayAt := time.Now().UTC().AddDate(0, -14, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "board_decay_refuted", Type: SignalBoardDecayConcern, Ticker: "COST",
			DetectedAt: decayAt, ValidThrough: validThru},
	}
	records := CorrelateBoardDecayConcernDeterioration(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTRefuted {
		t.Errorf("outcome = %s, want refuted", records[0].Outcome)
	}
}

func TestCorrelateBoardDecayConcernDeterioration_WrongTickerIgnored(t *testing.T) {
	decayAt := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 10, 0).Format("2006-01-02")
	fricAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "board_decay_wrong", Type: SignalBoardDecayConcern, Ticker: "HD",
			DetectedAt: decayAt, ValidThrough: validThru},
		{Type: SignalDirectorFriction, Ticker: "LOW", DetectedAt: fricAt},
	}
	records := CorrelateBoardDecayConcernDeterioration(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTPending {
		t.Errorf("outcome = %s, want pending (wrong ticker)", records[0].Outcome)
	}
}

// ── CorrelateDividendRaiseCapitalCluster ──────────────────────────────────────

func TestCorrelateDividendRaiseCapitalCluster_ConfirmedByBuyback(t *testing.T) {
	raiseAt := time.Now().UTC().AddDate(0, -3, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 9, 0).Format("2006-01-02")
	bbAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "div_raise_bb_test", Type: SignalDividendRaise, Ticker: "AAPL",
			DetectedAt: raiseAt, ValidThrough: validThru},
		{Type: SignalBuybackAuthorization, Ticker: "AAPL", DetectedAt: bbAt},
	}
	records := CorrelateDividendRaiseCapitalCluster(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed", records[0].Outcome)
	}
}

func TestCorrelateDividendRaiseCapitalCluster_ConfirmedByInsiderBuy(t *testing.T) {
	raiseAt := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 10, 0).Format("2006-01-02")
	buyAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "div_raise_ins_test", Type: SignalDividendRaise, Ticker: "MSFT",
			DetectedAt: raiseAt, ValidThrough: validThru},
		{Type: SignalInsiderBuy, Ticker: "MSFT", DetectedAt: buyAt},
	}
	records := CorrelateDividendRaiseCapitalCluster(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed", records[0].Outcome)
	}
}

func TestCorrelateDividendRaiseCapitalCluster_Refuted(t *testing.T) {
	raiseAt := time.Now().UTC().AddDate(0, -14, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "div_raise_refuted", Type: SignalDividendRaise, Ticker: "PG",
			DetectedAt: raiseAt, ValidThrough: validThru},
	}
	records := CorrelateDividendRaiseCapitalCluster(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTRefuted {
		t.Errorf("outcome = %s, want refuted", records[0].Outcome)
	}
}

func TestCorrelateDividendRaiseCapitalCluster_WrongTickerIgnored(t *testing.T) {
	raiseAt := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 10, 0).Format("2006-01-02")
	bbAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "div_raise_wrong", Type: SignalDividendRaise, Ticker: "KO",
			DetectedAt: raiseAt, ValidThrough: validThru},
		{Type: SignalBuybackAuthorization, Ticker: "PEP", DetectedAt: bbAt},
	}
	records := CorrelateDividendRaiseCapitalCluster(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTPending {
		t.Errorf("outcome = %s, want pending (wrong ticker)", records[0].Outcome)
	}
}

// ── CorrelateGovernanceDeterioratingDistress ──────────────────────────────────

func TestCorrelateGovernanceDeterioratingDistress_ConfirmedByCFODeparture(t *testing.T) {
	detAt := time.Now().UTC().AddDate(0, -3, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 9, 0).Format("2006-01-02")
	cfoAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "gov_det_cfo_test", Type: SignalGovernanceDeterioration, Ticker: "GE",
			DetectedAt: detAt, ValidThrough: validThru},
		{Type: SignalCFODeparture, Ticker: "GE", DetectedAt: cfoAt},
	}
	records := CorrelateGovernanceDeterioratingDistress(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed", records[0].Outcome)
	}
}

func TestCorrelateGovernanceDeterioratingDistress_ConfirmedByDirectorFriction(t *testing.T) {
	detAt := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 10, 0).Format("2006-01-02")
	fricAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "gov_det_fric_test", Type: SignalGovernanceDeterioration, Ticker: "F",
			DetectedAt: detAt, ValidThrough: validThru},
		{Type: SignalDirectorFriction, Ticker: "F", DetectedAt: fricAt},
	}
	records := CorrelateGovernanceDeterioratingDistress(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed", records[0].Outcome)
	}
}

func TestCorrelateGovernanceDeterioratingDistress_Refuted(t *testing.T) {
	detAt := time.Now().UTC().AddDate(0, -14, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "gov_det_refuted", Type: SignalGovernanceDeterioration, Ticker: "MMM",
			DetectedAt: detAt, ValidThrough: validThru},
	}
	records := CorrelateGovernanceDeterioratingDistress(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTRefuted {
		t.Errorf("outcome = %s, want refuted", records[0].Outcome)
	}
}

func TestCorrelateGovernanceDeterioratingDistress_WrongTickerIgnored(t *testing.T) {
	detAt := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 10, 0).Format("2006-01-02")
	cfoAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "gov_det_wrong", Type: SignalGovernanceDeterioration, Ticker: "IBM",
			DetectedAt: detAt, ValidThrough: validThru},
		{Type: SignalCFODeparture, Ticker: "HPQ", DetectedAt: cfoAt},
	}
	records := CorrelateGovernanceDeterioratingDistress(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTPending {
		t.Errorf("outcome = %s, want pending (wrong ticker)", records[0].Outcome)
	}
}

// ── CorrelateGovernanceImprovingCapitalReturn ─────────────────────────────────

func TestCorrelateGovernanceImprovingCapitalReturn_ConfirmedByDividendRaise(t *testing.T) {
	impAt := time.Now().UTC().AddDate(0, -3, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 9, 0).Format("2006-01-02")
	raiseAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "gov_imp_raise_test", Type: SignalGovernanceImproving, Ticker: "AMGN",
			DetectedAt: impAt, ValidThrough: validThru},
		{Type: SignalDividendRaise, Ticker: "AMGN", DetectedAt: raiseAt},
	}
	records := CorrelateGovernanceImprovingCapitalReturn(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed", records[0].Outcome)
	}
}

func TestCorrelateGovernanceImprovingCapitalReturn_ConfirmedByBuyback(t *testing.T) {
	impAt := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 10, 0).Format("2006-01-02")
	bbAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "gov_imp_bb_test", Type: SignalGovernanceImproving, Ticker: "UNH",
			DetectedAt: impAt, ValidThrough: validThru},
		{Type: SignalBuybackAuthorization, Ticker: "UNH", DetectedAt: bbAt},
	}
	records := CorrelateGovernanceImprovingCapitalReturn(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed", records[0].Outcome)
	}
}

func TestCorrelateGovernanceImprovingCapitalReturn_Refuted(t *testing.T) {
	impAt := time.Now().UTC().AddDate(0, -14, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "gov_imp_refuted", Type: SignalGovernanceImproving, Ticker: "CVX",
			DetectedAt: impAt, ValidThrough: validThru},
	}
	records := CorrelateGovernanceImprovingCapitalReturn(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTRefuted {
		t.Errorf("outcome = %s, want refuted", records[0].Outcome)
	}
}

func TestCorrelateGovernanceImprovingCapitalReturn_WrongTickerIgnored(t *testing.T) {
	impAt := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 10, 0).Format("2006-01-02")
	raiseAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "gov_imp_wrong", Type: SignalGovernanceImproving, Ticker: "XOM",
			DetectedAt: impAt, ValidThrough: validThru},
		{Type: SignalDividendRaise, Ticker: "CVX", DetectedAt: raiseAt},
	}
	records := CorrelateGovernanceImprovingCapitalReturn(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTPending {
		t.Errorf("outcome = %s, want pending (wrong ticker)", records[0].Outcome)
	}
}

// ── CorrelateGovernanceEntrenchmentVoteQuality ────────────────────────────────

func TestCorrelateGovernanceEntrenchmentVoteQuality_ConfirmedByCompensationConcern(t *testing.T) {
	entAt := time.Now().UTC().AddDate(0, -4, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 8, 0).Format("2006-01-02")
	compAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "entr_comp_test", Type: SignalGovernanceEntrenchment, Ticker: "MO",
			DetectedAt: entAt, ValidThrough: validThru},
		{Type: SignalCompensationConcern, Ticker: "MO", DetectedAt: compAt},
	}
	records := CorrelateGovernanceEntrenchmentVoteQuality(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed", records[0].Outcome)
	}
}

func TestCorrelateGovernanceEntrenchmentVoteQuality_ConfirmedByAbstentionSpike(t *testing.T) {
	entAt := time.Now().UTC().AddDate(0, -3, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 9, 0).Format("2006-01-02")
	abstAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "entr_abst_test", Type: SignalGovernanceEntrenchment, Ticker: "PM",
			DetectedAt: entAt, ValidThrough: validThru},
		{Type: SignalAbstentionSpike, Ticker: "PM", DetectedAt: abstAt},
	}
	records := CorrelateGovernanceEntrenchmentVoteQuality(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed", records[0].Outcome)
	}
}

func TestCorrelateGovernanceEntrenchmentVoteQuality_Refuted(t *testing.T) {
	entAt := time.Now().UTC().AddDate(0, -14, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "entr_refuted", Type: SignalGovernanceEntrenchment, Ticker: "RAD",
			DetectedAt: entAt, ValidThrough: validThru},
	}
	records := CorrelateGovernanceEntrenchmentVoteQuality(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTRefuted {
		t.Errorf("outcome = %s, want refuted", records[0].Outcome)
	}
}

func TestCorrelateGovernanceEntrenchmentVoteQuality_WrongTickerIgnored(t *testing.T) {
	entAt := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 10, 0).Format("2006-01-02")
	compAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "entr_wrong", Type: SignalGovernanceEntrenchment, Ticker: "LMT",
			DetectedAt: entAt, ValidThrough: validThru},
		{Type: SignalCompensationConcern, Ticker: "RTX", DetectedAt: compAt},
	}
	records := CorrelateGovernanceEntrenchmentVoteQuality(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTPending {
		t.Errorf("outcome = %s, want pending (wrong ticker)", records[0].Outcome)
	}
}

// ── CorrelateAbstentionOutlierNominationRejection ─────────────────────────────

func TestCorrelateAbstentionOutlierNominationRejection_Confirmed(t *testing.T) {
	outlierAt := time.Now().UTC().AddDate(0, -4, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 8, 0).Format("2006-01-02")
	rejAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "abst_out_nom_test", Type: SignalAbstentionOutlier, Ticker: "DUK",
			DetectedAt: outlierAt, ValidThrough: validThru},
		{Type: SignalNominationRejection, Ticker: "DUK", DetectedAt: rejAt},
	}
	records := CorrelateAbstentionOutlierNominationRejection(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed", records[0].Outcome)
	}
}

func TestCorrelateAbstentionOutlierNominationRejection_Refuted(t *testing.T) {
	outlierAt := time.Now().UTC().AddDate(0, -14, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "abst_out_refuted", Type: SignalAbstentionOutlier, Ticker: "SO",
			DetectedAt: outlierAt, ValidThrough: validThru},
	}
	records := CorrelateAbstentionOutlierNominationRejection(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTRefuted {
		t.Errorf("outcome = %s, want refuted", records[0].Outcome)
	}
}

func TestCorrelateAbstentionOutlierNominationRejection_Pending(t *testing.T) {
	outlierAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 11, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "abst_out_pending", Type: SignalAbstentionOutlier, Ticker: "NEE",
			DetectedAt: outlierAt, ValidThrough: validThru},
	}
	records := CorrelateAbstentionOutlierNominationRejection(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTPending {
		t.Errorf("outcome = %s, want pending", records[0].Outcome)
	}
}

func TestCorrelateAbstentionOutlierNominationRejection_WrongTickerIgnored(t *testing.T) {
	outlierAt := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 10, 0).Format("2006-01-02")
	rejAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "abst_out_wrong", Type: SignalAbstentionOutlier, Ticker: "AEP",
			DetectedAt: outlierAt, ValidThrough: validThru},
		{Type: SignalNominationRejection, Ticker: "PPL", DetectedAt: rejAt},
	}
	records := CorrelateAbstentionOutlierNominationRejection(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTPending {
		t.Errorf("outcome = %s, want pending (wrong ticker)", records[0].Outcome)
	}
}

// ── CorrelatePostFailureActivistPrediction ────────────────────────────────────

func TestCorrelatePostFailureActivistPrediction_Confirmed(t *testing.T) {
	predAt := time.Now().UTC().AddDate(0, -4, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 8, 0).Format("2006-01-02")
	actAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "pf_act_pred_test", Type: SignalPostFailureActivistPrediction, Ticker: "NRG",
			DetectedAt: predAt, ValidThrough: validThru},
		{Type: SignalActivistRisk, Ticker: "NRG", DetectedAt: actAt},
	}
	records := CorrelatePostFailureActivistPrediction(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed", records[0].Outcome)
	}
}

func TestCorrelatePostFailureActivistPrediction_Refuted(t *testing.T) {
	predAt := time.Now().UTC().AddDate(0, -14, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "pf_act_pred_refuted", Type: SignalPostFailureActivistPrediction, Ticker: "EIX",
			DetectedAt: predAt, ValidThrough: validThru},
	}
	records := CorrelatePostFailureActivistPrediction(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTRefuted {
		t.Errorf("outcome = %s, want refuted", records[0].Outcome)
	}
}

func TestCorrelatePostFailureActivistPrediction_Pending(t *testing.T) {
	predAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 11, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "pf_act_pred_pending", Type: SignalPostFailureActivistPrediction, Ticker: "PCG",
			DetectedAt: predAt, ValidThrough: validThru},
	}
	records := CorrelatePostFailureActivistPrediction(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTPending {
		t.Errorf("outcome = %s, want pending", records[0].Outcome)
	}
}

func TestCorrelatePostFailureActivistPrediction_WrongTickerIgnored(t *testing.T) {
	predAt := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 10, 0).Format("2006-01-02")
	actAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "pf_act_pred_wrong", Type: SignalPostFailureActivistPrediction, Ticker: "AES",
			DetectedAt: predAt, ValidThrough: validThru},
		{Type: SignalActivistRisk, Ticker: "CMS", DetectedAt: actAt},
	}
	records := CorrelatePostFailureActivistPrediction(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTPending {
		t.Errorf("outcome = %s, want pending (wrong ticker)", records[0].Outcome)
	}
}

// ── CorrelateBuybackAuthorizationInsiderBuy ───────────────────────────────────

func TestCorrelateBuybackAuthorizationInsiderBuy_Confirmed(t *testing.T) {
	bbAt := time.Now().UTC().AddDate(0, -3, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 9, 0).Format("2006-01-02")
	buyAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "bb_auth_ins_test", Type: SignalBuybackAuthorization, Ticker: "GOOG",
			DetectedAt: bbAt, ValidThrough: validThru},
		{Type: SignalInsiderBuy, Ticker: "GOOG", DetectedAt: buyAt},
	}
	records := CorrelateBuybackAuthorizationInsiderBuy(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed", records[0].Outcome)
	}
}

func TestCorrelateBuybackAuthorizationInsiderBuy_Refuted(t *testing.T) {
	bbAt := time.Now().UTC().AddDate(0, -14, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "bb_auth_refuted", Type: SignalBuybackAuthorization, Ticker: "META",
			DetectedAt: bbAt, ValidThrough: validThru},
	}
	records := CorrelateBuybackAuthorizationInsiderBuy(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTRefuted {
		t.Errorf("outcome = %s, want refuted", records[0].Outcome)
	}
}

func TestCorrelateBuybackAuthorizationInsiderBuy_Pending(t *testing.T) {
	bbAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 11, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "bb_auth_pending", Type: SignalBuybackAuthorization, Ticker: "AMZN",
			DetectedAt: bbAt, ValidThrough: validThru},
	}
	records := CorrelateBuybackAuthorizationInsiderBuy(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTPending {
		t.Errorf("outcome = %s, want pending", records[0].Outcome)
	}
}

func TestCorrelateBuybackAuthorizationInsiderBuy_WrongTickerIgnored(t *testing.T) {
	bbAt := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 10, 0).Format("2006-01-02")
	buyAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "bb_auth_wrong", Type: SignalBuybackAuthorization, Ticker: "NVDA",
			DetectedAt: bbAt, ValidThrough: validThru},
		{Type: SignalInsiderBuy, Ticker: "AMD", DetectedAt: buyAt},
	}
	records := CorrelateBuybackAuthorizationInsiderBuy(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTPending {
		t.Errorf("outcome = %s, want pending (wrong ticker)", records[0].Outcome)
	}
}

// ── CorrelateBrokerNonVoteAnomalyDirectorFriction ─────────────────────────────

func TestCorrelateBrokerNonVoteAnomalyDirectorFriction_Confirmed(t *testing.T) {
	nonVoteAt := time.Now().UTC().AddDate(0, -4, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 8, 0).Format("2006-01-02")
	fricAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "broker_fric_test", Type: SignalBrokerNonVoteAnomaly, Ticker: "SLB",
			DetectedAt: nonVoteAt, ValidThrough: validThru},
		{Type: SignalDirectorFriction, Ticker: "SLB", DetectedAt: fricAt},
	}
	records := CorrelateBrokerNonVoteAnomalyDirectorFriction(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed", records[0].Outcome)
	}
}

func TestCorrelateBrokerNonVoteAnomalyDirectorFriction_Refuted(t *testing.T) {
	nonVoteAt := time.Now().UTC().AddDate(0, -14, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "broker_refuted", Type: SignalBrokerNonVoteAnomaly, Ticker: "HAL",
			DetectedAt: nonVoteAt, ValidThrough: validThru},
	}
	records := CorrelateBrokerNonVoteAnomalyDirectorFriction(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTRefuted {
		t.Errorf("outcome = %s, want refuted", records[0].Outcome)
	}
}

func TestCorrelateBrokerNonVoteAnomalyDirectorFriction_Pending(t *testing.T) {
	nonVoteAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 11, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "broker_pending", Type: SignalBrokerNonVoteAnomaly, Ticker: "BKR",
			DetectedAt: nonVoteAt, ValidThrough: validThru},
	}
	records := CorrelateBrokerNonVoteAnomalyDirectorFriction(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTPending {
		t.Errorf("outcome = %s, want pending", records[0].Outcome)
	}
}

func TestCorrelateBrokerNonVoteAnomalyDirectorFriction_WrongTickerIgnored(t *testing.T) {
	nonVoteAt := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 10, 0).Format("2006-01-02")
	fricAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "broker_wrong", Type: SignalBrokerNonVoteAnomaly, Ticker: "MRO",
			DetectedAt: nonVoteAt, ValidThrough: validThru},
		{Type: SignalDirectorFriction, Ticker: "OXY", DetectedAt: fricAt},
	}
	records := CorrelateBrokerNonVoteAnomalyDirectorFriction(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTPending {
		t.Errorf("outcome = %s, want pending (wrong ticker)", records[0].Outcome)
	}
}

func TestCorrelateSpecialDividendCapitalReturn_Confirmed(t *testing.T) {
	specDivAt := time.Now().UTC().AddDate(0, -3, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 9, 0).Format("2006-01-02")
	bbAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "specdiv_1", Type: SignalSpecialDividend, Ticker: "NUE",
			DetectedAt: specDivAt, ValidThrough: validThru},
		{Type: SignalBuybackAuthorization, Ticker: "NUE", DetectedAt: bbAt},
	}
	records := CorrelateSpecialDividendCapitalReturn(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed", records[0].Outcome)
	}
	if records[0].EvidenceDate != bbAt {
		t.Errorf("evidence date = %s, want %s", records[0].EvidenceDate, bbAt)
	}
}

func TestCorrelateSpecialDividendCapitalReturn_Refuted(t *testing.T) {
	specDivAt := time.Now().UTC().AddDate(0, -14, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "specdiv_2", Type: SignalSpecialDividend, Ticker: "NUE",
			DetectedAt: specDivAt, ValidThrough: validThru},
	}
	records := CorrelateSpecialDividendCapitalReturn(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTRefuted {
		t.Errorf("outcome = %s, want refuted (expired window)", records[0].Outcome)
	}
}

func TestCorrelateSpecialDividendCapitalReturn_ConfirmedInsiderBuy(t *testing.T) {
	specDivAt := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 10, 0).Format("2006-01-02")
	insiderAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "specdiv_3", Type: SignalSpecialDividend, Ticker: "PKG",
			DetectedAt: specDivAt, ValidThrough: validThru},
		{Type: SignalInsiderBuy, Ticker: "PKG", DetectedAt: insiderAt},
	}
	records := CorrelateSpecialDividendCapitalReturn(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed (insider_buy)", records[0].Outcome)
	}
}

func TestCorrelateSpecialDividendCapitalReturn_WrongTickerIgnored(t *testing.T) {
	specDivAt := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 10, 0).Format("2006-01-02")
	bbAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "specdiv_wrong", Type: SignalSpecialDividend, Ticker: "NUE",
			DetectedAt: specDivAt, ValidThrough: validThru},
		{Type: SignalBuybackAuthorization, Ticker: "PKG", DetectedAt: bbAt},
	}
	records := CorrelateSpecialDividendCapitalReturn(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTPending {
		t.Errorf("outcome = %s, want pending (wrong ticker)", records[0].Outcome)
	}
}

func TestCorrelateEPSFilingRevisionDistress_Confirmed(t *testing.T) {
	epsAt := time.Now().UTC().AddDate(0, -3, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 9, 0).Format("2006-01-02")
	cfoAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "eps_1", Type: SignalEPSFilingRevision, Ticker: "GE",
			DetectedAt: epsAt, ValidThrough: validThru},
		{Type: SignalCFODeparture, Ticker: "GE", DetectedAt: cfoAt},
	}
	records := CorrelateEPSFilingRevisionDistress(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed", records[0].Outcome)
	}
	if records[0].EvidenceDate != cfoAt {
		t.Errorf("evidence date = %s, want %s", records[0].EvidenceDate, cfoAt)
	}
}

func TestCorrelateEPSFilingRevisionDistress_Refuted(t *testing.T) {
	epsAt := time.Now().UTC().AddDate(0, -14, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "eps_2", Type: SignalEPSFilingRevision, Ticker: "GE",
			DetectedAt: epsAt, ValidThrough: validThru},
	}
	records := CorrelateEPSFilingRevisionDistress(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTRefuted {
		t.Errorf("outcome = %s, want refuted (expired window)", records[0].Outcome)
	}
}

func TestCorrelateEPSFilingRevisionDistress_ConfirmedDividendCut(t *testing.T) {
	epsAt := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 10, 0).Format("2006-01-02")
	divCutAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "eps_3", Type: SignalEPSFilingRevision, Ticker: "F",
			DetectedAt: epsAt, ValidThrough: validThru},
		{Type: SignalDividendCut, Ticker: "F", DetectedAt: divCutAt},
	}
	records := CorrelateEPSFilingRevisionDistress(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed (dividend_cut)", records[0].Outcome)
	}
}

func TestCorrelateEPSFilingRevisionDistress_WrongTickerIgnored(t *testing.T) {
	epsAt := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 10, 0).Format("2006-01-02")
	lateAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "eps_wrong", Type: SignalEPSFilingRevision, Ticker: "GE",
			DetectedAt: epsAt, ValidThrough: validThru},
		{Type: SignalLateFiling, Ticker: "F", DetectedAt: lateAt},
	}
	records := CorrelateEPSFilingRevisionDistress(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTPending {
		t.Errorf("outcome = %s, want pending (wrong ticker)", records[0].Outcome)
	}
}

func TestCorrelateCompensationConcernEscalation_Confirmed(t *testing.T) {
	concernAt := time.Now().UTC().AddDate(0, -3, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 9, 0).Format("2006-01-02")
	abstAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "comp_1", Type: SignalCompensationConcern, Ticker: "CBS",
			DetectedAt: concernAt, ValidThrough: validThru},
		{Type: SignalAbstentionSpike, Ticker: "CBS", DetectedAt: abstAt},
	}
	records := CorrelateCompensationConcernEscalation(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed", records[0].Outcome)
	}
	if records[0].EvidenceDate != abstAt {
		t.Errorf("evidence date = %s, want %s", records[0].EvidenceDate, abstAt)
	}
}

func TestCorrelateCompensationConcernEscalation_Refuted(t *testing.T) {
	concernAt := time.Now().UTC().AddDate(0, -14, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "comp_2", Type: SignalCompensationConcern, Ticker: "CBS",
			DetectedAt: concernAt, ValidThrough: validThru},
	}
	records := CorrelateCompensationConcernEscalation(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTRefuted {
		t.Errorf("outcome = %s, want refuted (expired window)", records[0].Outcome)
	}
}

func TestCorrelateCompensationConcernEscalation_ConfirmedNomRejection(t *testing.T) {
	concernAt := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 10, 0).Format("2006-01-02")
	rejAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "comp_3", Type: SignalCompensationConcern, Ticker: "DIS",
			DetectedAt: concernAt, ValidThrough: validThru},
		{Type: SignalNominationRejection, Ticker: "DIS", DetectedAt: rejAt},
	}
	records := CorrelateCompensationConcernEscalation(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed (nomination_rejection)", records[0].Outcome)
	}
}

func TestCorrelateCompensationConcernEscalation_WrongTickerIgnored(t *testing.T) {
	concernAt := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 10, 0).Format("2006-01-02")
	abstAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "comp_wrong", Type: SignalCompensationConcern, Ticker: "CBS",
			DetectedAt: concernAt, ValidThrough: validThru},
		{Type: SignalAbstentionSpike, Ticker: "DIS", DetectedAt: abstAt},
	}
	records := CorrelateCompensationConcernEscalation(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTPending {
		t.Errorf("outcome = %s, want pending (wrong ticker)", records[0].Outcome)
	}
}

func TestCorrelateNominationRejectionFriction_Confirmed(t *testing.T) {
	rejAt := time.Now().UTC().AddDate(0, -3, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 9, 0).Format("2006-01-02")
	fricAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "nomrej_1", Type: SignalNominationRejection, Ticker: "HCA",
			DetectedAt: rejAt, ValidThrough: validThru},
		{Type: SignalDirectorFriction, Ticker: "HCA", DetectedAt: fricAt},
	}
	records := CorrelateNominationRejectionFriction(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed", records[0].Outcome)
	}
	if records[0].EvidenceDate != fricAt {
		t.Errorf("evidence date = %s, want %s", records[0].EvidenceDate, fricAt)
	}
}

func TestCorrelateNominationRejectionFriction_Refuted(t *testing.T) {
	rejAt := time.Now().UTC().AddDate(0, -14, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "nomrej_2", Type: SignalNominationRejection, Ticker: "HCA",
			DetectedAt: rejAt, ValidThrough: validThru},
	}
	records := CorrelateNominationRejectionFriction(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTRefuted {
		t.Errorf("outcome = %s, want refuted (expired window)", records[0].Outcome)
	}
}

func TestCorrelateNominationRejectionFriction_ConfirmedAbstentionSpike(t *testing.T) {
	rejAt := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 10, 0).Format("2006-01-02")
	abstAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "nomrej_3", Type: SignalNominationRejection, Ticker: "BAX",
			DetectedAt: rejAt, ValidThrough: validThru},
		{Type: SignalAbstentionSpike, Ticker: "BAX", DetectedAt: abstAt},
	}
	records := CorrelateNominationRejectionFriction(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed (abstention_spike)", records[0].Outcome)
	}
}

func TestCorrelateNominationRejectionFriction_WrongTickerIgnored(t *testing.T) {
	rejAt := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 10, 0).Format("2006-01-02")
	fricAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "nomrej_wrong", Type: SignalNominationRejection, Ticker: "HCA",
			DetectedAt: rejAt, ValidThrough: validThru},
		{Type: SignalDirectorFriction, Ticker: "BAX", DetectedAt: fricAt},
	}
	records := CorrelateNominationRejectionFriction(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTPending {
		t.Errorf("outcome = %s, want pending (wrong ticker)", records[0].Outcome)
	}
}

func TestCorrelateHighTrustDirectorStability_Confirmed(t *testing.T) {
	htAt := time.Now().UTC().AddDate(0, -3, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 9, 0).Format("2006-01-02")
	govAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "ht_1", Type: SignalHighTrustDirector, Ticker: "ITW",
			DetectedAt: htAt, ValidThrough: validThru},
		{Type: SignalGovernanceImproving, Ticker: "ITW", DetectedAt: govAt},
	}
	records := CorrelateHighTrustDirectorStability(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed", records[0].Outcome)
	}
	if records[0].EvidenceDate != govAt {
		t.Errorf("evidence date = %s, want %s", records[0].EvidenceDate, govAt)
	}
}

func TestCorrelateHighTrustDirectorStability_Refuted(t *testing.T) {
	htAt := time.Now().UTC().AddDate(0, -14, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "ht_2", Type: SignalHighTrustDirector, Ticker: "ITW",
			DetectedAt: htAt, ValidThrough: validThru},
	}
	records := CorrelateHighTrustDirectorStability(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTRefuted {
		t.Errorf("outcome = %s, want refuted (expired window)", records[0].Outcome)
	}
}

func TestCorrelateHighTrustDirectorStability_ConfirmedBuyback(t *testing.T) {
	htAt := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 10, 0).Format("2006-01-02")
	bbAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "ht_3", Type: SignalHighTrustDirector, Ticker: "ROK",
			DetectedAt: htAt, ValidThrough: validThru},
		{Type: SignalBuybackAuthorization, Ticker: "ROK", DetectedAt: bbAt},
	}
	records := CorrelateHighTrustDirectorStability(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed (buyback_authorization)", records[0].Outcome)
	}
}

func TestCorrelateHighTrustDirectorStability_WrongTickerIgnored(t *testing.T) {
	htAt := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 10, 0).Format("2006-01-02")
	govAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "ht_wrong", Type: SignalHighTrustDirector, Ticker: "ITW",
			DetectedAt: htAt, ValidThrough: validThru},
		{Type: SignalGovernanceImproving, Ticker: "ROK", DetectedAt: govAt},
	}
	records := CorrelateHighTrustDirectorStability(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTPending {
		t.Errorf("outcome = %s, want pending (wrong ticker)", records[0].Outcome)
	}
}

func TestCorrelateFamilyControlEntrenchment_Confirmed(t *testing.T) {
	famAt := time.Now().UTC().AddDate(0, -3, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 9, 0).Format("2006-01-02")
	entrAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "fam_1", Type: SignalFamilyControl, Ticker: "GOOG",
			DetectedAt: famAt, ValidThrough: validThru},
		{Type: SignalGovernanceEntrenchment, Ticker: "GOOG", DetectedAt: entrAt},
	}
	records := CorrelateFamilyControlEntrenchment(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed", records[0].Outcome)
	}
	if records[0].EvidenceDate != entrAt {
		t.Errorf("evidence date = %s, want %s", records[0].EvidenceDate, entrAt)
	}
}

func TestCorrelateFamilyControlEntrenchment_Refuted(t *testing.T) {
	famAt := time.Now().UTC().AddDate(0, -14, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "fam_2", Type: SignalFamilyControl, Ticker: "GOOG",
			DetectedAt: famAt, ValidThrough: validThru},
	}
	records := CorrelateFamilyControlEntrenchment(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTRefuted {
		t.Errorf("outcome = %s, want refuted (expired window)", records[0].Outcome)
	}
}

func TestCorrelateFamilyControlEntrenchment_ConfirmedCompConcern(t *testing.T) {
	famAt := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 10, 0).Format("2006-01-02")
	compAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "fam_3", Type: SignalFamilyControl, Ticker: "META",
			DetectedAt: famAt, ValidThrough: validThru},
		{Type: SignalCompensationConcern, Ticker: "META", DetectedAt: compAt},
	}
	records := CorrelateFamilyControlEntrenchment(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed (compensation_concern)", records[0].Outcome)
	}
}

func TestCorrelateFamilyControlEntrenchment_WrongTickerIgnored(t *testing.T) {
	famAt := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 10, 0).Format("2006-01-02")
	entrAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "fam_wrong", Type: SignalFamilyControl, Ticker: "GOOG",
			DetectedAt: famAt, ValidThrough: validThru},
		{Type: SignalGovernanceEntrenchment, Ticker: "META", DetectedAt: entrAt},
	}
	records := CorrelateFamilyControlEntrenchment(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTPending {
		t.Errorf("outcome = %s, want pending (wrong ticker)", records[0].Outcome)
	}
}

func TestCorrelateDirectorLinkContagion_Confirmed(t *testing.T) {
	linkAt := time.Now().UTC().AddDate(0, -3, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 9, 0).Format("2006-01-02")
	fricAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "link_1", Type: SignalDirectorLink, Ticker: "VFC",
			DetectedAt: linkAt, ValidThrough: validThru},
		{Type: SignalDirectorFriction, Ticker: "VFC", DetectedAt: fricAt},
	}
	records := CorrelateDirectorLinkContagion(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed", records[0].Outcome)
	}
	if records[0].EvidenceDate != fricAt {
		t.Errorf("evidence date = %s, want %s", records[0].EvidenceDate, fricAt)
	}
}

func TestCorrelateDirectorLinkContagion_Refuted(t *testing.T) {
	linkAt := time.Now().UTC().AddDate(0, -14, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "link_2", Type: SignalDirectorLink, Ticker: "VFC",
			DetectedAt: linkAt, ValidThrough: validThru},
	}
	records := CorrelateDirectorLinkContagion(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTRefuted {
		t.Errorf("outcome = %s, want refuted (expired window)", records[0].Outcome)
	}
}

func TestCorrelateDirectorLinkContagion_ConfirmedAbstentionSpike(t *testing.T) {
	linkAt := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 10, 0).Format("2006-01-02")
	abstAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "link_3", Type: SignalDirectorLink, Ticker: "PVH",
			DetectedAt: linkAt, ValidThrough: validThru},
		{Type: SignalAbstentionSpike, Ticker: "PVH", DetectedAt: abstAt},
	}
	records := CorrelateDirectorLinkContagion(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed (abstention_spike)", records[0].Outcome)
	}
}

func TestCorrelateDirectorLinkContagion_WrongTickerIgnored(t *testing.T) {
	linkAt := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 10, 0).Format("2006-01-02")
	fricAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "link_wrong", Type: SignalDirectorLink, Ticker: "VFC",
			DetectedAt: linkAt, ValidThrough: validThru},
		{Type: SignalDirectorFriction, Ticker: "PVH", DetectedAt: fricAt},
	}
	records := CorrelateDirectorLinkContagion(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTPending {
		t.Errorf("outcome = %s, want pending (wrong ticker)", records[0].Outcome)
	}
}

func TestCorrelateGovernancePeerUnderperformerDeterioration_Confirmed(t *testing.T) {
	peerAt := time.Now().UTC().AddDate(0, -3, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 9, 0).Format("2006-01-02")
	detAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "peer_1", Type: SignalGovernancePeerUnderperformer, Ticker: "GT",
			DetectedAt: peerAt, ValidThrough: validThru},
		{Type: SignalGovernanceDeterioration, Ticker: "GT", DetectedAt: detAt},
	}
	records := CorrelateGovernancePeerUnderperformerDeterioration(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed", records[0].Outcome)
	}
	if records[0].EvidenceDate != detAt {
		t.Errorf("evidence date = %s, want %s", records[0].EvidenceDate, detAt)
	}
}

func TestCorrelateGovernancePeerUnderperformerDeterioration_Refuted(t *testing.T) {
	peerAt := time.Now().UTC().AddDate(0, -14, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "peer_2", Type: SignalGovernancePeerUnderperformer, Ticker: "GT",
			DetectedAt: peerAt, ValidThrough: validThru},
	}
	records := CorrelateGovernancePeerUnderperformerDeterioration(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTRefuted {
		t.Errorf("outcome = %s, want refuted (expired window)", records[0].Outcome)
	}
}

func TestCorrelateGovernancePeerUnderperformerDeterioration_ConfirmedBoardDecay(t *testing.T) {
	peerAt := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 10, 0).Format("2006-01-02")
	decayAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "peer_3", Type: SignalGovernancePeerUnderperformer, Ticker: "LEA",
			DetectedAt: peerAt, ValidThrough: validThru},
		{Type: SignalBoardDecayConcern, Ticker: "LEA", DetectedAt: decayAt},
	}
	records := CorrelateGovernancePeerUnderperformerDeterioration(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed (board_decay_concern)", records[0].Outcome)
	}
}

func TestCorrelateGovernancePeerUnderperformerDeterioration_WrongTickerIgnored(t *testing.T) {
	peerAt := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 10, 0).Format("2006-01-02")
	detAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "peer_wrong", Type: SignalGovernancePeerUnderperformer, Ticker: "GT",
			DetectedAt: peerAt, ValidThrough: validThru},
		{Type: SignalGovernanceDeterioration, Ticker: "LEA", DetectedAt: detAt},
	}
	records := CorrelateGovernancePeerUnderperformerDeterioration(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTPending {
		t.Errorf("outcome = %s, want pending (wrong ticker)", records[0].Outcome)
	}
}

func TestCorrelateGovernanceHealthIndexStability_Confirmed(t *testing.T) {
	ghAt := time.Now().UTC().AddDate(0, -3, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 9, 0).Format("2006-01-02")
	govAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "gh_1", Type: SignalGovernanceHealth, Ticker: "MMM",
			DetectedAt: ghAt, ValidThrough: validThru},
		{Type: SignalGovernanceImproving, Ticker: "MMM", DetectedAt: govAt},
	}
	records := CorrelateGovernanceHealthIndexStability(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed", records[0].Outcome)
	}
	if records[0].EvidenceDate != govAt {
		t.Errorf("evidence date = %s, want %s", records[0].EvidenceDate, govAt)
	}
}

func TestCorrelateGovernanceHealthIndexStability_Refuted(t *testing.T) {
	ghAt := time.Now().UTC().AddDate(0, -14, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "gh_2", Type: SignalGovernanceHealth, Ticker: "MMM",
			DetectedAt: ghAt, ValidThrough: validThru},
	}
	records := CorrelateGovernanceHealthIndexStability(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTRefuted {
		t.Errorf("outcome = %s, want refuted (expired window)", records[0].Outcome)
	}
}

func TestCorrelateGovernanceHealthIndexStability_ConfirmedBuyback(t *testing.T) {
	ghAt := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 10, 0).Format("2006-01-02")
	bbAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "gh_3", Type: SignalGovernanceHealth, Ticker: "CAT",
			DetectedAt: ghAt, ValidThrough: validThru},
		{Type: SignalBuybackAuthorization, Ticker: "CAT", DetectedAt: bbAt},
	}
	records := CorrelateGovernanceHealthIndexStability(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed (buyback_authorization)", records[0].Outcome)
	}
}

func TestCorrelateGovernanceHealthIndexStability_WrongTickerIgnored(t *testing.T) {
	ghAt := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 10, 0).Format("2006-01-02")
	govAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "gh_wrong", Type: SignalGovernanceHealth, Ticker: "MMM",
			DetectedAt: ghAt, ValidThrough: validThru},
		{Type: SignalGovernanceImproving, Ticker: "CAT", DetectedAt: govAt},
	}
	records := CorrelateGovernanceHealthIndexStability(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTPending {
		t.Errorf("outcome = %s, want pending (wrong ticker)", records[0].Outcome)
	}
}

func TestCorrelateDirectorLongTenureEntrenchment_Confirmed(t *testing.T) {
	tenAt := time.Now().UTC().AddDate(0, -3, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 9, 0).Format("2006-01-02")
	entrAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "ten_1", Type: SignalDirectorLongTenure, Ticker: "WBA",
			DetectedAt: tenAt, ValidThrough: validThru},
		{Type: SignalGovernanceEntrenchment, Ticker: "WBA", DetectedAt: entrAt},
	}
	records := CorrelateDirectorLongTenureEntrenchment(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed", records[0].Outcome)
	}
	if records[0].EvidenceDate != entrAt {
		t.Errorf("evidence date = %s, want %s", records[0].EvidenceDate, entrAt)
	}
}

func TestCorrelateDirectorLongTenureEntrenchment_Refuted(t *testing.T) {
	tenAt := time.Now().UTC().AddDate(0, -14, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "ten_2", Type: SignalDirectorLongTenure, Ticker: "WBA",
			DetectedAt: tenAt, ValidThrough: validThru},
	}
	records := CorrelateDirectorLongTenureEntrenchment(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTRefuted {
		t.Errorf("outcome = %s, want refuted (expired window)", records[0].Outcome)
	}
}

func TestCorrelateDirectorLongTenureEntrenchment_ConfirmedCompConcern(t *testing.T) {
	tenAt := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 10, 0).Format("2006-01-02")
	compAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "ten_3", Type: SignalDirectorLongTenure, Ticker: "JNJ",
			DetectedAt: tenAt, ValidThrough: validThru},
		{Type: SignalCompensationConcern, Ticker: "JNJ", DetectedAt: compAt},
	}
	records := CorrelateDirectorLongTenureEntrenchment(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTConfirmed {
		t.Errorf("outcome = %s, want confirmed (compensation_concern)", records[0].Outcome)
	}
}

func TestCorrelateDirectorLongTenureEntrenchment_WrongTickerIgnored(t *testing.T) {
	tenAt := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")
	validThru := time.Now().UTC().AddDate(0, 10, 0).Format("2006-01-02")
	entrAt := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		{SignalID: "ten_wrong", Type: SignalDirectorLongTenure, Ticker: "WBA",
			DetectedAt: tenAt, ValidThrough: validThru},
		{Type: SignalGovernanceEntrenchment, Ticker: "JNJ", DetectedAt: entrAt},
	}
	records := CorrelateDirectorLongTenureEntrenchment(sigs)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Outcome != GTPending {
		t.Errorf("outcome = %s, want pending (wrong ticker)", records[0].Outcome)
	}
}
