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
	processed, err := pollOnce(filepath.Join(dir, "latest.json"), filepath.Join(dir, ".last-processed"), "true", "", true)
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
	writeObs(t, latest, observation{
		Timestamp: "2026-05-28T12:00:00Z",
		Summary:   "stalled processor",
		Severity:  "warn",
		Findings:  "no events for 30m",
	})

	processed, err := pollOnce(latest, cursor, "true", "", true)
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
	if string(got) != "2026-05-28T12:00:00Z" {
		t.Errorf("cursor = %q, want timestamp", string(got))
	}
}

func TestPollOnceSkipsAlreadyProcessed(t *testing.T) {
	dir := t.TempDir()
	latest := filepath.Join(dir, "latest.json")
	cursor := filepath.Join(dir, ".last-processed")
	writeObs(t, latest, observation{
		Timestamp: "2026-05-28T12:00:00Z",
		Summary:   "stalled processor",
		Findings:  "x",
	})
	if err := os.WriteFile(cursor, []byte("2026-05-28T12:00:00Z"), 0o644); err != nil {
		t.Fatal(err)
	}
	processed, err := pollOnce(latest, cursor, "true", "", true)
	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	if processed {
		t.Error("processed should be false when timestamp matches cursor")
	}
}

func TestPollOnceProcessesNewerObservation(t *testing.T) {
	dir := t.TempDir()
	latest := filepath.Join(dir, "latest.json")
	cursor := filepath.Join(dir, ".last-processed")
	if err := os.WriteFile(cursor, []byte("2026-05-28T11:00:00Z"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeObs(t, latest, observation{
		Timestamp: "2026-05-28T12:00:00Z",
		Summary:   "new issue",
		Findings:  "y",
	})
	processed, err := pollOnce(latest, cursor, "true", "", true)
	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	if !processed {
		t.Error("processed should be true when timestamp differs")
	}
	got, _ := os.ReadFile(cursor)
	if string(got) != "2026-05-28T12:00:00Z" {
		t.Errorf("cursor not updated; got %q", string(got))
	}
}

func TestPollOnceRejectsMissingTimestamp(t *testing.T) {
	dir := t.TempDir()
	latest := filepath.Join(dir, "latest.json")
	// Write a JSON object with no timestamp.
	if err := os.WriteFile(latest, []byte(`{"summary":"x","findings":"y"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := pollOnce(latest, filepath.Join(dir, ".last-processed"), "true", "", true)
	if err == nil {
		t.Fatal("expected error for missing timestamp")
	}
	if !strings.Contains(err.Error(), "timestamp") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestBuildPromptContainsKeyFields(t *testing.T) {
	p := buildPrompt("var/emily-observations/latest.json", observation{
		Summary:  "ticker dropped",
		Severity: "error",
	})
	for _, want := range []string{"var/emily-observations/latest.json", "ticker dropped", "error", "go test"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q: %s", want, p)
		}
	}
}

func TestSplitArgs(t *testing.T) {
	cases := map[string][]string{
		"":                              nil,
		"   ":                           nil,
		"--foo":                         {"--foo"},
		"--foo --bar baz":               {"--foo", "--bar", "baz"},
		"  --dangerously-skip-permissions  ": {"--dangerously-skip-permissions"},
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
