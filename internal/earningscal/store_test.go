package earningscal

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRefresh_FiltersFiscalYearZero verifies that legacy records with
// FiscalYear==0 (a since-fixed extraction bug, see cmd/earnings-calendar's
// scanSecFilings) are excluded on load, so a corrected record appended
// under a different ID isn't shadowed by a stale, broken duplicate.
func TestRefresh_FiltersFiscalYearZero(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dates.ndjson")
	lines := `{"id":"earncal-AAPL-FY-0","ticker":"AAPL","fiscal_quarter":"FY","fiscal_year":0,"report_date":"2003-10-15","status":"confirmed","source":"8k_filing"}
{"id":"earncal-AAPL-FY-2003","ticker":"AAPL","fiscal_quarter":"FY","fiscal_year":2003,"report_date":"2003-10-15","status":"confirmed","source":"8k_filing"}
`
	if err := os.WriteFile(path, []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}

	s := NewStore(dir)
	if err := s.Refresh(); err != nil {
		t.Fatal(err)
	}

	all := s.All()
	if len(all) != 1 {
		t.Fatalf("expected 1 record after filtering FiscalYear=0, got %d: %+v", len(all), all)
	}
	if all[0].FiscalYear != 2003 {
		t.Fatalf("expected the surviving record to have FiscalYear=2003, got %d", all[0].FiscalYear)
	}
}
