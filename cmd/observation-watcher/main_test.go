package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	processed, err := pollOnce(filepath.Join(dir, "latest.json"), filepath.Join(dir, ".last-processed"), "true", "", true, "", "none", nil)
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

	processed, err := pollOnce(latest, cursor, "true", "", true, "", "none", nil)
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
	if _, err := pollOnce(latest, cursor, "true", "", true, "", "none", nil); err != nil {
		t.Fatalf("poll1: %v", err)
	}

	second := first
	second.Timestamp = "2026-05-28T12:05:00Z"
	writeObs(t, latest, second)
	processed, err := pollOnce(latest, cursor, "true", "", true, "", "none", nil)
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
	if _, err := pollOnce(latest, cursor, "true", "", true, "", "none", nil); err != nil {
		t.Fatalf("poll1: %v", err)
	}

	second := observation{
		Timestamp: "2026-05-28T12:00:00Z",
		Summary:   "different issue",
		Findings:  "ticker dropped",
	}
	writeObs(t, latest, second)
	processed, err := pollOnce(latest, cursor, "true", "", true, "", "none", nil)
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
	if _, err := pollOnce(latest, cursor, "true", "", true, "", "none", nil); err != nil {
		t.Fatalf("poll1: %v", err)
	}

	second := first
	second.Severity = "error"
	second.Timestamp = "2026-05-28T11:01:00Z"
	writeObs(t, latest, second)
	processed, err := pollOnce(latest, cursor, "true", "", true, "", "none", nil)
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
	_, err := pollOnce(latest, filepath.Join(dir, ".last-processed"), "true", "", true, "", "none", nil)
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
	processed, err := pollOnce(latest, cursor, "false", "", false, "", "nontrivial", nil)
	if err != nil {
		// "false" command returns exit status 1 — if we get here, the gate failed.
		t.Fatalf("gate did not suppress trivial observation (command was invoked): %v", err)
	}
	if !processed {
		t.Error("expected processed=true (cursor updated) even when gate suppresses invocation")
	}
}

func TestPollOnceTrivialGateAllowsErrorSeverity(t *testing.T) {
	// Regression: Emily's hand-written observations set severity=error but leave
	// entity-graph fields (status, gaps, parse_errors, signals_by_type) empty.
	// The gate was treating these as trivial because it only checked entity-graph
	// fields. Any observation with severity != "" and != "ok" must pass through.
	dir := t.TempDir()
	latest := filepath.Join(dir, "latest.json")
	cursor := filepath.Join(dir, ".last-processed")

	obs := observation{
		Timestamp: "2026-05-30T09:46:52Z",
		Severity:  "error",
		Summary:   "eps-processor ticker map has only 2 entries — all press releases are being dropped silently",
		Findings:  "source: emily\nstatus: needs_attention\n...",
	}
	writeObs(t, latest, obs)

	processed, err := pollOnce(latest, cursor, "true", "", true, "", "nontrivial", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !processed {
		t.Error("severity=error observation must not be gated as trivial")
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
	processed, err := pollOnce(latest, cursor, "true", "", true, "", "nontrivial", nil)
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

func TestBuildPromptEntityGraphSkipsRulesWhenNoGaps(t *testing.T) {
	// When an entity-graph observation has no gaps, no parse errors, and no
	// rule-change request, the rules file must NOT be inlined (saves ~320T/dispatch).
	dir := t.TempDir()
	rulesPath := filepath.Join(dir, "rules.json")
	if err := os.WriteFile(rulesPath, []byte(`{"friction_threshold":0.85}`), 0o644); err != nil {
		t.Fatal(err)
	}

	obs := observation{
		Timestamp:        "2026-06-10T10:00:00Z",
		Source:           "entity-graph",
		Status:           "ok",
		Subject:          "Entity graph run: 2 filings, 6 directors, 6 signals",
		FilingsProcessed: 2,
		DirectorsFound:   6,
		SignalsGenerated: 6,
		SignalsByType:    map[string]int{"director_friction": 2, "high_trust_director": 4},
		// No Gaps, no ParseErrors, no RequestForClaude mentioning rules/thresholds.
	}
	p := buildPrompt("var/emily-observations/latest.json", obs, rulesPath)

	if strings.Contains(p, "friction_threshold") {
		t.Error("rules file must not be inlined when observation has no gaps or parse errors")
	}
	if strings.Contains(p, "Current Signal Rules") {
		t.Error("Current Signal Rules section must be absent when no actionable gaps")
	}
	// Core fields must still appear.
	for _, want := range []string{"entity-graph", "6 signals", "go test"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing expected field %q", want)
		}
	}
}

func TestBuildPromptEntityGraphIncludesRulesForRuleChangeRequest(t *testing.T) {
	// RequestForClaude mentioning "rule" or "threshold" must trigger rules inlining
	// even without gaps/parse errors.
	dir := t.TempDir()
	rulesPath := filepath.Join(dir, "rules.json")
	if err := os.WriteFile(rulesPath, []byte(`{"friction_threshold":0.85}`), 0o644); err != nil {
		t.Fatal(err)
	}

	obs := observation{
		Timestamp:        "2026-06-10T10:00:00Z",
		Source:           "entity-graph",
		Status:           "ok",
		Subject:          "Entity graph run",
		FilingsProcessed: 1,
		SignalsGenerated: 2,
		RequestForClaude: "Should the friction_threshold rule be lowered to 0.80?",
	}
	p := buildPrompt("var/emily-observations/latest.json", obs, rulesPath)

	if !strings.Contains(p, "friction_threshold") {
		t.Error("rules file must be inlined when RequestForClaude mentions rule changes")
	}
}

func writePrimeTask(t *testing.T, dir, filename, taskType, description string) {
	t.Helper()
	b, _ := json.Marshal(primeTask{TaskID: "t1", TaskType: taskType, Description: description})
	if err := os.WriteFile(filepath.Join(dir, filename), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPrimeTaskDuplicateExists(t *testing.T) {
	dir := t.TempDir()

	// Write a recent task with type "rsi_report" and a specific description.
	recent := "2099-01-01T120000Z-task-111.json"
	writePrimeTask(t, dir, recent, "rsi_report", "analyse token spend")

	// Same type+description → duplicate detected.
	if !primeTaskDuplicateExists(dir, "2099-01-01T120005Z-task-222.json", "rsi_report", "analyse token spend", 4*time.Hour) {
		t.Error("expected duplicate to be detected for matching type+description")
	}

	// Different description → not a duplicate.
	if primeTaskDuplicateExists(dir, "2099-01-01T120005Z-task-222.json", "rsi_report", "different task", 4*time.Hour) {
		t.Error("expected no duplicate for different description")
	}

	// Different type → not a duplicate.
	if primeTaskDuplicateExists(dir, "2099-01-01T120005Z-task-222.json", "other_type", "analyse token spend", 4*time.Hour) {
		t.Error("expected no duplicate for different task_type")
	}

	// Self-check: a file should not match itself.
	if primeTaskDuplicateExists(dir, recent, "rsi_report", "analyse token spend", 4*time.Hour) {
		t.Error("file should not match itself")
	}
}

func TestIsCreditExhaustedOutput(t *testing.T) {
	cases := map[string]bool{
		"Credit balance is too low":                  true,
		"⚠ credit balance is too low\nplease top up": true,
		"CREDIT BALANCE IS TOO LOW":                  true,
		"rate limit exceeded, please retry":          false,
		"":                                           false,
		"context length exceeded":                    false,
	}
	for in, want := range cases {
		if got := isCreditExhaustedOutput(in); got != want {
			t.Errorf("isCreditExhaustedOutput(%q) = %v, want %v", in, got, want)
		}
	}
}

// resetCreditCooldown clears the package-level circuit-breaker state before
// and after a test that manipulates it, so tests don't leak state into each
// other (main()'s poll loop is single-threaded in production, and these
// tests run sequentially too, but this keeps each test self-contained).
func resetCreditCooldown(t *testing.T) {
	t.Helper()
	creditExhaustedUntil = time.Time{}
	t.Cleanup(func() { creditExhaustedUntil = time.Time{} })
}

func TestInvokeWithRetry_CooldownActive_SkipsWithoutRunning(t *testing.T) {
	resetCreditCooldown(t)
	creditExhaustedUntil = time.Now().Add(5 * time.Minute)

	// A command that would fail loudly if actually executed — proves the
	// cooldown short-circuits before exec.Command ever runs.
	err := invokeWithRetry("/definitely/does/not/exist/anywhere", nil)
	if !errors.Is(err, errCreditCooldown) {
		t.Fatalf("expected errCreditCooldown while cooldown is active, got: %v", err)
	}
}

func TestInvokeWithRetry_DetectsCreditExhausted_TripsCooldown(t *testing.T) {
	resetCreditCooldown(t)
	if creditCooldownActive() {
		t.Fatal("cooldown should not be active before any failure")
	}

	// "sh -c" running a script that prints the exact phrase claude emits and
	// exits non-zero — simulates a real claude invocation without depending
	// on the claude CLI or network/credits.
	err := invokeWithRetry("sh", []string{"-c", "echo 'Credit balance is too low'; exit 1"})
	if !errors.Is(err, errCreditCooldown) {
		t.Fatalf("expected errCreditCooldown after detecting the message, got: %v", err)
	}
	if !creditCooldownActive() {
		t.Fatal("expected cooldown to be active immediately after detection")
	}

	// A second call, even against a command that would otherwise succeed,
	// must short-circuit — proves the breaker actually prevents further
	// subprocess spawns until the cooldown expires.
	err = invokeWithRetry("true", nil)
	if !errors.Is(err, errCreditCooldown) {
		t.Fatalf("expected the breaker to skip a subsequent call, got: %v", err)
	}
}

func TestInvokeWithRetry_UnrelatedFailureDoesNotTripCooldown(t *testing.T) {
	resetCreditCooldown(t)

	err := invokeWithRetry("sh", []string{"-c", "echo 'some other error'; exit 1"})
	if err == nil {
		t.Fatal("expected an error from a failing command")
	}
	if errors.Is(err, errCreditCooldown) {
		t.Fatal("an unrelated failure must not trip the credit-cooldown breaker")
	}
	if creditCooldownActive() {
		t.Fatal("cooldown must not be active after an unrelated failure")
	}
}

func TestSplitArgs(t *testing.T) {
	cases := map[string][]string{
		"":                                   nil,
		"   ":                                nil,
		"--foo":                              {"--foo"},
		"--foo --bar baz":                    {"--foo", "--bar", "baz"},
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

// TestPollBatchedCapsLargeBatchAndAdvancesCursorPartially is the regression test for a real
// bug found live 2026-08-11: a batch of 149 new observations (78 nontrivial) built one prompt
// too large for exec.Command's underlying fork/exec, failing with the OS-level "argument list
// too long" (E2BIG) -- an error invokeWithRetry's own output-sniffing never catches (the process
// never starts, so there's no output to sniff), so the batch failed immediately, the cursor
// never advanced (it only ever advances on success), and the exact same oversized batch would
// have retried and failed identically forever. See pollBatched's own maxBatchObs doc comment
// for the full writeup. This test proves both halves of the fix: (1) a batch larger than
// maxBatchObs gets capped, not passed through whole, and (2) the cursor advances only to the
// last INCLUDED observation, not the true newest file, so the remainder is picked up (not
// silently skipped) on the next poll.
func TestPollBatchedCapsLargeBatchAndAdvancesCursorPartially(t *testing.T) {
	dir := t.TempDir()
	obsDir := filepath.Join(dir, "observations")
	cursor := filepath.Join(dir, ".batch-cursor")

	const total = maxBatchObs + 5
	var names []string
	for i := 0; i < total; i++ {
		name := fmt.Sprintf("2026-08-11T00-00-%02dZ.json", i)
		writeObs(t, filepath.Join(obsDir, name), observation{
			Timestamp: fmt.Sprintf("2026-08-11T00:00:%02dZ", i),
			Summary:   fmt.Sprintf("observation %d", i),
			Severity:  "info",
		})
		names = append(names, name)
	}

	// dryRun=true, cmdName="true", gateMode="none": every observation counts as nontrivial,
	// no real claude invocation happens, but the cursor still advances exactly as a real
	// successful invocation would (same convention TestPollOnceProcessesNewObservation already
	// relies on for pollOnce).
	processed, err := pollBatched(obsDir, cursor, "true", "", true, "", "none", time.Hour, nil)
	if err != nil {
		t.Fatalf("pollBatched: %v", err)
	}
	if !processed {
		t.Fatal("expected processed=true")
	}

	got, err := os.ReadFile(cursor)
	if err != nil {
		t.Fatalf("cursor missing: %v", err)
	}
	wantCursor := names[maxBatchObs-1]
	if string(got) != wantCursor {
		t.Errorf("cursor after first pass = %q, want %q (capped batch must not advance past the last INCLUDED observation)", string(got), wantCursor)
	}

	// Second poll should pick up exactly the remaining (total - maxBatchObs) observations.
	processed2, err := pollBatched(obsDir, cursor, "true", "", true, "", "none", time.Hour, nil)
	if err != nil {
		t.Fatalf("second pollBatched: %v", err)
	}
	if !processed2 {
		t.Fatal("expected second pollBatched to process the remaining observations")
	}
	got2, err := os.ReadFile(cursor)
	if err != nil {
		t.Fatalf("cursor missing after second pass: %v", err)
	}
	if string(got2) != names[total-1] {
		t.Errorf("cursor after second pass = %q, want %q (remainder fully drained, not re-capped forever)", string(got2), names[total-1])
	}
}
