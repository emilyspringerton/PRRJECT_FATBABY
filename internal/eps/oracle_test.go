package eps

import (
	"os"
	"testing"
	"time"
)

func tmpDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "eps-oracle-test-*")
	if err != nil {
		t.Fatalf("mkdirtemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func ptr(v float64) *float64 { return &v }

func TestOracleRoundTrip(t *testing.T) {
	dir := tmpDir(t)
	now := time.Now().UTC().Format("2006-01-02")

	cases := []OracleCase{
		{
			CaseID:         "c1",
			SourceIdentity: "aapl:press:001",
			Ticker:         "AAPL",
			Period:         EarningsPeriod{FiscalQuarter: "Q1", FiscalYear: 2026, PeriodEnd: "2026-03-31"},
			ExtractedEPS:   ptr(2.40),
			Verdict:        VerdictPending,
			RecordedAt:     now,
		},
		{
			CaseID:         "c2",
			SourceIdentity: "msft:press:002",
			Ticker:         "MSFT",
			Period:         EarningsPeriod{FiscalQuarter: "Q2", FiscalYear: 2026, PeriodEnd: "2026-12-31"},
			ExtractedEPS:   ptr(3.23),
			FiledEPS:       ptr(3.23),
			Verdict:        VerdictConfirmed,
			RecordedAt:     now,
		},
	}

	for _, c := range cases {
		if err := AppendOracleCase(dir, c); err != nil {
			t.Fatalf("AppendOracleCase: %v", err)
		}
	}

	loaded, err := LoadOracleCases(dir)
	if err != nil {
		t.Fatalf("LoadOracleCases: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("want 2 cases, got %d", len(loaded))
	}
	if loaded[0].CaseID != "c1" {
		t.Errorf("case[0].CaseID = %q, want c1", loaded[0].CaseID)
	}
	if loaded[1].Verdict != VerdictConfirmed {
		t.Errorf("case[1].Verdict = %q, want confirmed", loaded[1].Verdict)
	}
}

func TestLoadOracleCases_Missing(t *testing.T) {
	dir := tmpDir(t)
	cases, err := LoadOracleCases(dir)
	if err != nil {
		t.Fatalf("want nil error for missing file, got %v", err)
	}
	if cases != nil {
		t.Errorf("want nil slice for missing file, got %v", cases)
	}
}

func TestWriteOracleCases_Rewrite(t *testing.T) {
	dir := tmpDir(t)
	now := time.Now().UTC().Format("2006-01-02")

	orig := []OracleCase{
		{CaseID: "c1", Ticker: "AAPL", Verdict: VerdictPending, RecordedAt: now},
		{CaseID: "c2", Ticker: "MSFT", Verdict: VerdictPending, RecordedAt: now},
	}
	for _, c := range orig {
		if err := AppendOracleCase(dir, c); err != nil {
			t.Fatal(err)
		}
	}

	// Update c1 to confirmed.
	updated := []OracleCase{
		{CaseID: "c1", Ticker: "AAPL", Verdict: VerdictConfirmed, RecordedAt: now},
		{CaseID: "c2", Ticker: "MSFT", Verdict: VerdictPending, RecordedAt: now},
	}
	if err := WriteOracleCases(dir, updated); err != nil {
		t.Fatalf("WriteOracleCases: %v", err)
	}

	loaded, err := LoadOracleCases(dir)
	if err != nil {
		t.Fatalf("LoadOracleCases after rewrite: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("want 2 cases after rewrite, got %d", len(loaded))
	}
	if loaded[0].Verdict != VerdictConfirmed {
		t.Errorf("c1 verdict after rewrite = %q, want confirmed", loaded[0].Verdict)
	}
}

func TestBuildOracleSummary(t *testing.T) {
	now := time.Now().UTC().Format("2006-01-02")
	cases := []OracleCase{
		{CaseID: "a", Verdict: VerdictConfirmed, RecordedAt: now},
		{CaseID: "b", Verdict: VerdictConfirmed, RecordedAt: now},
		{CaseID: "c", Verdict: VerdictContradicts, RecordedAt: now},
		{CaseID: "d", Verdict: VerdictPending, RecordedAt: now},
	}
	s := BuildOracleSummary(cases)

	if s.Total != 4 {
		t.Errorf("Total = %d, want 4", s.Total)
	}
	if s.Confirmed != 2 {
		t.Errorf("Confirmed = %d, want 2", s.Confirmed)
	}
	if s.Contradicts != 1 {
		t.Errorf("Contradicts = %d, want 1", s.Contradicts)
	}
	if s.Pending != 1 {
		t.Errorf("Pending = %d, want 1", s.Pending)
	}
	// Precision = 2 / (2+1) ≈ 0.667
	if s.Precision < 0.66 || s.Precision > 0.68 {
		t.Errorf("Precision = %.4f, want ~0.667", s.Precision)
	}
}

func TestBuildOracleSummary_AllPending(t *testing.T) {
	cases := []OracleCase{
		{CaseID: "a", Verdict: VerdictPending},
		{CaseID: "b", Verdict: VerdictPending},
	}
	s := BuildOracleSummary(cases)
	if s.Precision != 0 {
		t.Errorf("Precision = %.4f, want 0 when no resolved cases", s.Precision)
	}
}
