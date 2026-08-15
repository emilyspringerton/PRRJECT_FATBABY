package fabledata

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/example/prrject-fatbaby/internal/eps"
)

func f64(v float64) *float64 { return &v }

func writeNDJSON(t *testing.T, path string, lines []interface{}) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()
	for _, l := range lines {
		b, err := json.Marshal(l)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if _, err := f.Write(append(b, '\n')); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
}

func TestBuildExamples_OnlyResolvedVerdictsIncluded(t *testing.T) {
	dir := t.TempDir()
	writeNDJSON(t, filepath.Join(dir, "articles.ndjson"), []interface{}{
		article{SourceIdentity: "sid-1", Ticker: "AAPL", Headline: "Apple posts EPS", Dek: "dek1"},
		article{SourceIdentity: "sid-2", Ticker: "MSFT", Headline: "Microsoft posts EPS", Dek: "dek2"},
		article{SourceIdentity: "sid-3", Ticker: "GOOG", Headline: "Google posts EPS", Dek: "dek3"},
		article{SourceIdentity: "sid-4", Ticker: "NVDA", Headline: "Nvidia posts EPS", Dek: "dek4"},
	})
	writeNDJSON(t, filepath.Join(dir, "oracle.ndjson"), []interface{}{
		eps.OracleCase{CaseID: "c1", SourceIdentity: "sid-1", Verdict: eps.VerdictConfirmed, ExtractedEPS: f64(1.5), FiledEPS: f64(1.5), RecordedAt: "2026-08-15T00:00:00Z"},
		eps.OracleCase{CaseID: "c2", SourceIdentity: "sid-2", Verdict: eps.VerdictContradicts, ExtractedEPS: f64(2.0), FiledEPS: f64(1.9), RecordedAt: "2026-08-15T00:00:00Z"},
		eps.OracleCase{CaseID: "c3", SourceIdentity: "sid-3", Verdict: eps.VerdictPending, RecordedAt: "2026-08-15T00:00:00Z"},
		eps.OracleCase{CaseID: "c4", SourceIdentity: "sid-4", Verdict: eps.VerdictUnresolvable, RecordedAt: "2026-08-15T00:00:00Z"},
	})

	examples, err := BuildExamples(dir, nil)
	if err != nil {
		t.Fatalf("BuildExamples: %v", err)
	}
	if len(examples) != 2 {
		t.Fatalf("expected 2 graded examples (pending/unresolvable excluded), got %d", len(examples))
	}
	for _, e := range examples {
		if e.Verdict != string(eps.VerdictConfirmed) && e.Verdict != string(eps.VerdictContradicts) {
			t.Errorf("unexpected verdict in output: %s", e.Verdict)
		}
		if e.LicenseClass != LicenseClassOwnExhaust {
			t.Errorf("LicenseClass = %q, want %q", e.LicenseClass, LicenseClassOwnExhaust)
		}
		if e.OracleName != OracleNameEPSReconciler {
			t.Errorf("OracleName = %q, want %q", e.OracleName, OracleNameEPSReconciler)
		}
	}
}

func TestBuildExamples_MissingArticleSkipped(t *testing.T) {
	dir := t.TempDir()
	writeNDJSON(t, filepath.Join(dir, "articles.ndjson"), []interface{}{
		article{SourceIdentity: "sid-1", Ticker: "AAPL", Headline: "Apple posts EPS"},
	})
	writeNDJSON(t, filepath.Join(dir, "oracle.ndjson"), []interface{}{
		eps.OracleCase{CaseID: "c1", SourceIdentity: "sid-1", Verdict: eps.VerdictConfirmed, RecordedAt: "2026-08-15T00:00:00Z"},
		// sid-2 has a resolved verdict but no matching article -- a real
		// gap, must be skipped rather than fabricating partial text.
		eps.OracleCase{CaseID: "c2", SourceIdentity: "sid-2", Verdict: eps.VerdictConfirmed, RecordedAt: "2026-08-15T00:00:00Z"},
	})

	examples, err := BuildExamples(dir, nil)
	if err != nil {
		t.Fatalf("BuildExamples: %v", err)
	}
	if len(examples) != 1 {
		t.Fatalf("expected 1 example (missing article skipped), got %d", len(examples))
	}
	if examples[0].SourceIdentity != "sid-1" {
		t.Errorf("SourceIdentity = %q, want sid-1", examples[0].SourceIdentity)
	}
}

func TestBuildExamples_TombstonesExcluded(t *testing.T) {
	dir := t.TempDir()
	writeNDJSON(t, filepath.Join(dir, "articles.ndjson"), []interface{}{
		article{SourceIdentity: "sid-1", Ticker: "AAPL", Headline: "Apple posts EPS"},
	})
	writeNDJSON(t, filepath.Join(dir, "oracle.ndjson"), []interface{}{
		eps.OracleCase{CaseID: "c1", SourceIdentity: "sid-1", Verdict: eps.VerdictConfirmed, RecordedAt: "2026-08-15T00:00:00Z"},
	})

	id := contentHash("sid-1")
	examples, err := BuildExamples(dir, map[string]bool{id: true})
	if err != nil {
		t.Fatalf("BuildExamples: %v", err)
	}
	if len(examples) != 0 {
		t.Fatalf("expected 0 examples (tombstoned), got %d", len(examples))
	}
}

func TestBuildExamples_DeterministicSortOrder(t *testing.T) {
	dir := t.TempDir()
	writeNDJSON(t, filepath.Join(dir, "articles.ndjson"), []interface{}{
		article{SourceIdentity: "sid-z", Ticker: "ZZZZ", Headline: "z"},
		article{SourceIdentity: "sid-a", Ticker: "AAAA", Headline: "a"},
	})
	writeNDJSON(t, filepath.Join(dir, "oracle.ndjson"), []interface{}{
		eps.OracleCase{CaseID: "c1", SourceIdentity: "sid-z", Verdict: eps.VerdictConfirmed, RecordedAt: "2026-08-15T00:00:00Z"},
		eps.OracleCase{CaseID: "c2", SourceIdentity: "sid-a", Verdict: eps.VerdictConfirmed, RecordedAt: "2026-08-15T00:00:00Z"},
	})

	examples, err := BuildExamples(dir, nil)
	if err != nil {
		t.Fatalf("BuildExamples: %v", err)
	}
	if len(examples) != 2 {
		t.Fatalf("expected 2 examples, got %d", len(examples))
	}
	if examples[0].ID >= examples[1].ID {
		t.Errorf("examples not sorted by ID: %s should come before %s", examples[0].ID, examples[1].ID)
	}
}

func TestBuildExamples_EmptyCorpusNoError(t *testing.T) {
	dir := t.TempDir()
	examples, err := BuildExamples(dir, nil)
	if err != nil {
		t.Fatalf("BuildExamples on empty dir should not error: %v", err)
	}
	if len(examples) != 0 {
		t.Errorf("expected 0 examples, got %d", len(examples))
	}
}
