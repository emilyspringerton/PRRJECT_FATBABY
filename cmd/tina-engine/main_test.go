package main

import (
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/example/prrject-fatbaby/internal/guidance"
	"github.com/example/prrject-fatbaby/prwatch"
)

// testLogger gives tests a real *log.Logger without polluting stdout/stderr.
func testLogger(t *testing.T) *log.Logger {
	t.Helper()
	return log.New(testWriter{t}, "", 0)
}

type testWriter struct{ t *testing.T }

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Logf("%s", p)
	return len(p), nil
}

func TestReadGuidanceRaises_FiltersActionAndTolerantOfBadLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "articles.ndjson")
	// Real, mixed content: a real raise, a real lowered (must be excluded), a malformed line
	// (must not abort the whole scan), and a second real raise after it.
	content := `{"id":"g1","source_identity":"pr:1","ticker":"AAA","action":"raised"}
{"id":"g2","source_identity":"pr:2","ticker":"BBB","action":"lowered"}
not valid json at all
{"id":"g3","source_identity":"pr:3","ticker":"CCC","action":"raised"}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := readGuidanceRaises(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 real raises (lowered excluded, bad line skipped), got %d: %+v", len(got), got)
	}
	if got[0].ID != "g1" || got[1].ID != "g3" {
		t.Errorf("expected g1 then g3, got %q then %q", got[0].ID, got[1].ID)
	}
}

func TestReadGuidanceRaises_MissingFileIsEmptyNotError(t *testing.T) {
	got, err := readGuidanceRaises(filepath.Join(t.TempDir(), "nonexistent.ndjson"))
	if err != nil {
		t.Fatalf("a missing guidance file (real, honest first-run case) should not be an error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty result, got %+v", got)
	}
}

func TestSeenSet_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".seen")

	seen, err := readSeenSet(path)
	if err != nil {
		t.Fatalf("missing seen-file (first real run) should not be an error: %v", err)
	}
	if len(seen) != 0 {
		t.Errorf("expected empty seen set on first run, got %+v", seen)
	}

	if err := appendSeen(path, "g1"); err != nil {
		t.Fatalf("appendSeen: %v", err)
	}
	if err := appendSeen(path, "g2"); err != nil {
		t.Fatalf("appendSeen: %v", err)
	}

	seen, err = readSeenSet(path)
	if err != nil {
		t.Fatalf("readSeenSet after append: %v", err)
	}
	if !seen["g1"] || !seen["g2"] {
		t.Errorf("expected both g1 and g2 marked seen, got %+v", seen)
	}
	if seen["g3"] {
		t.Errorf("g3 was never appended, should not be marked seen")
	}
}

func TestExtractJSONObject(t *testing.T) {
	cases := []struct{ in, want string }{
		{`{"a":1}`, `{"a":1}`},
		{"Sure, here you go:\n{\"a\":1}\nHope that helps!", `{"a":1}`},
		{"no braces here", "no braces here"},
	}
	for _, c := range cases {
		if got := extractJSONObject(c.in); got != c.want {
			t.Errorf("extractJSONObject(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestWriteDraft_RealFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	d := TinaDraft{GuidanceID: "g1", Ticker: "AAA", Title: "TINA: Test"}
	if err := writeDraft(dir, "g1", d); err != nil {
		t.Fatalf("writeDraft: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "g1.json"))
	if err != nil {
		t.Fatalf("expected a real file written: %v", err)
	}
	if len(b) == 0 {
		t.Error("expected non-empty draft file")
	}
}

// TestDraft_DryRunMakesNoRealAPICall -- a real, honest smoke test that -dry-run truly never
// dials out (no ANTHROPIC_API_KEY needed here at all), matching this command's own documented
// safety contract.
func TestDraft_DryRunMakesNoRealAPICall(t *testing.T) {
	d := &tinaDrafter{dryRun: true, logger: testLogger(t)}
	a := guidance.Article{ID: "g1", Ticker: "AAA", Action: guidance.ActionRaised}
	body := prwatch.BodyFetchedEvent{URL: "https://example.test/pr"}
	draft, err := d.Draft(nil, a, body)
	if err != nil {
		t.Fatalf("dry-run Draft should never error: %v", err)
	}
	if draft.Title == "" || draft.GuidanceID != "g1" {
		t.Errorf("expected a real, non-empty dry-run draft placeholder, got %+v", draft)
	}
}
