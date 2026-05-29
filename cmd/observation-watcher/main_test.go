package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeObs(t *testing.T, path string, obs observation) {
	t.Helper()
	b, err := json.Marshal(obs)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPollOnceNoFile(t *testing.T) {
	dir := t.TempDir()
	processed, err := pollOnce(filepath.Join(dir, "latest.json"), filepath.Join(dir, ".last-processed"), "true", "", true, "", "none")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if processed {
		t.Error("processed should be false when no file present")
	}
}

func TestPollOnceProcessesNewObservation(t *testing.T) {
	dir := t.TempDir()
	latest := filepath.Join(dir, "latest.json")
	cursor := filepath.Join(dir, ".last-processed")
	obs := observation{
		Timestamp: "2026-05-28T12:00:00Z",
		Summary:   "stalled processor",
		Severity:  "warn",
		Findings:  "no events for 30m",
	}
	writeObs(t, latest, obs)

	processed, err := pollOnce(latest, cursor, "true", "", true, "", "none")
	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	if !processed {
		t.Fatal("expected processed=true on first sighting")
	}
	got, err := os.ReadFile(cursor)
	if err != nil {
		t.Fatalf("cursor missing: %v", err)
	}
	if string(got) != observationHash(obs) {
		t.Errorf("cursor = %q, want hash", string(got))
	}
}

func TestPollOnceSkipsSameContentEvenIfTimestampChanged(t *testing.T) {
	// Emily may re-publish the same finding on every tick. Watcher must
	// dedupe by content, not just timestamp — otherwise Claude Code gets
	// re-triggered on every tick with no real new work to do.
	dir := t.TempDir()
	latest := filepath.Join(dir, "latest.json")
	cursor := filepath.Join(dir, ".last-processed")
	first := observation{
		Timestamp: "2026-05-28T12:00:00Z",
		Summary:   "stalled processor",
		Severity:  "warn",
		Findings:  "no events for 30m",
	}
	writeObs(t, latest, first)
	if _, err := pollOnce(latest, cursor, "true", "", true, "", "none"); err != nil {
		t.Fatalf("poll1: %v", err)
	}

	second := first
	second.Timestamp = "2026-05-28T12:05:00Z"
	writeObs(t, latest, second)
	processed, err := pollOnce(latest, cursor, "true", "", true, "", "none")
	if err != nil {
		t.Fatalf("poll2: %v", err)
	}
	if processed {
		t.Error("expected dedupe: same content, only timestamp changed")
	}
}

func TestPollOnceProcessesContentChange(t *testing.T) {
	dir := t.TempDir()
	latest := filepath.Join(dir, "latest.json")
	cursor := filepath.Join(dir, ".last-processed")
	first := observation{
		Timestamp: "2026-05-28T11:00:00Z",
		Summary:   "stalled processor",
		Findings:  "no events for 30m",
	}
	writeObs(t, latest, first)
	if _, err := pollOnce(latest, cursor, "true", "", true, "", "none"); err != nil {
		t.Fatalf("poll1: %v", err)
	}

	second := observation{
		Timestamp: "2026-05-28T12:00:00Z",
		Summary:   "different issue",
		Findings:  "ticker dropped",
	}
	writeObs(t, latest, second)
	processed, err := pollOnce(latest, cursor, "true", "", true, "", "none")
	if err != nil {
		t.Fatalf("poll2: %v", err)
	}
	if !processed {
		t.Error("expected processed=true when content changed")
	}
	got, _ := os.ReadFile(cursor)
	if string(got) != observationHash(second) {
		t.Errorf("cursor not updated to new hash")
	}
}

func TestPollOnceProcessesSeverityChange(t *testing.T) {
	// Same summary/findings but escalating severity must re-trigger.
	dir := t.TempDir()
	latest := filepath.Join(dir, "latest.json")
	cursor := filepath.Join(dir, ".last-processed")
	first := observation{
		Timestamp: "2026-05-28T11:00:00Z",
		Severity:  "warn",
		Summary:   "stalled processor",
		Findings:  "no events for 30m",
	}
	writeObs(t, latest, first)
	if _, err := pollOnce(latest, cursor, "true", "", true, "", "none"); err != nil {
		t.Fatalf("poll1: %v", err)
	}

	second := first
	second.Severity = "error"
	second.Timestamp = "2026-05-28T11:01:00Z"
	writeObs(t, latest, second)
	processed, err := pollOnce(latest, cursor, "true", "", true, "", "none")
	if err != nil {
		t.Fatalf("poll2: %v", err)
	}
	if !processed {
		t.Error("expected severity escalation to re-trigger")
	}
}

func TestPollOnceRejectsMissingTimestamp(t *testing.T) {
	dir := t.TempDir()
	latest := filepath.Join(dir, "latest.json")
	// Write a JSON object with no timestamp.
	if err := os.WriteFile(latest, []byte(`{"summary":"x","findings":"y"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := pollOnce(latest, filepath.Join(dir, ".last-processed"), "true", "", true, "", "none")
	if err == nil {
		t.Fatal("expected error for missing timestamp")
	}
	if !strings.Contains(err.Error(), "timestamp") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestPollOnceTrivialGateSkipsInvocation(t *testing.T) {
	dir := t.TempDir()
	latest := filepath.Join(dir, "latest.json")
	cursor := filepath.Join(dir, ".last-processed")

	// Observation with only high_trust signals, no gaps, status ok.
	obs := observation{
		Timestamp:     "2026-05-29T18:00:00Z",
		Source:        "entity-graph",
		Status:        "ok",
		Subject:       "Entity graph run: 1 filings, 4 directors, 4 signals",
		SignalsByType: map[string]int{"high_trust_director": 4},
	}
	writeObs(t, latest, obs)

	// gate=nontrivial + dry_run=false: should NOT invoke the command (use "false"
	// which exits 1 to confirm invocation would have failed).
	processed, err := pollOnce(latest, cursor, "false", "", false, "", "nontrivial")
	if err != nil {
		// "false" command returns exit status 1 — if we get here, the gate failed.
		t.Fatalf("gate did not suppress trivial observation (command was invoked): %v", err)
	}
	if !processed {
		t.Error("expected processed=true (cursor updated) even when gate suppresses invocation")
	}
}

func TestPollOnceTrivialGateAllowsNonTrivial(t *testing.T) {
	dir := t.TempDir()
	latest := filepath.Join(dir, "latest.json")
	cursor := filepath.Join(dir, ".last-processed")

	// Observation with a friction signal — not trivial.
	obs := observation{
		Timestamp:     "2026-05-29T18:00:00Z",
		Source:        "entity-graph",
		Status:        "needs_attention",
		Subject:       "friction detected",
		SignalsByType: map[string]int{"high_trust_director": 3, "director_friction": 1},
		Gaps:          []string{"some gap"},
	}
	writeObs(t, latest, obs)

	// gate=nontrivial + dry_run=true: should proceed to invocation path (dry-run won't actually run).
	processed, err := pollOnce(latest, cursor, "true", "", true, "", "nontrivial")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !processed {
		t.Error("expected processed=true for non-trivial observation with nontrivial gate")
	}
}

func TestBuildPromptContainsKeyFields(t *testing.T) {
	p := buildPrompt("var/emily-observations/latest.json", observation{
		Summary:  "ticker dropped",
		Severity: "error",
	}, "")
	for _, want := range []string{"var/emily-observations/latest.json", "ticker dropped", "error", "go test"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q: %s", want, p)
		}
	}
}

func TestBuildPromptEntityGraph(t *testing.T) {
	// Write a temporary rules file so the prompt inlines it.
	dir := t.TempDir()
	rulesPath := filepath.Join(dir, "rules.json")
	if err := os.WriteFile(rulesPath, []byte(`{"friction_threshold":0.85}`), 0o644); err != nil {
		t.Fatal(err)
	}

	obs := observation{
		Timestamp:        "2026-05-29T10:00:00Z",
		Source:           "entity-graph",
		Status:           "needs_attention",
		Subject:          "Entity graph run: 1 filings, 4 directors, 3 signals",
		FilingsProcessed: 1,
		DirectorsFound:   4,
		SignalsGenerated: 3,
		SignalsByType:    map[string]int{"director_friction": 1, "high_trust_director": 2},
		Gaps:             []string{"No entrenchment signals detected"},
		RequestForClaude: "Should entrenchment_min_for be lowered?",
	}
	p := buildPrompt("var/emily-observations/latest.json", obs, rulesPath)

	for _, want := range []string{
		"entity-graph",
		"needs_attention",
		"4 directors",
		"No entrenchment signals detected",
		"Should entrenchment_min_for be lowered",
		"friction_threshold",
		"config/entity-graph-rules.json",
		"go test",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("entity-graph prompt missing %q", want)
		}
	}
}

func TestSplitArgs(t *testing.T) {
	cases := map[string][]string{
		"":                                    nil,
		"   ":                                 nil,
		"--foo":                               {"--foo"},
		"--foo --bar baz":                     {"--foo", "--bar", "baz"},
		"  --dangerously-skip-permissions  ":  {"--dangerously-skip-permissions"},
	}
	for in, want := range cases {
		got := splitArgs(in)
		if len(got) != len(want) {
			t.Errorf("splitArgs(%q) = %v, want %v", in, got, want)
			continue
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("splitArgs(%q)[%d] = %q, want %q", in, i, got[i], want[i])
			}
		}
	}
}
