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
