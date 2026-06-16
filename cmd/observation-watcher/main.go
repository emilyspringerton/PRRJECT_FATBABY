// observation-watcher polls var/emily-observations/latest.json and, when a
// new observation appears (distinguished by content hash), invokes the
// configured command — typically `claude` — with a prompt that tells it to
// act on the observation. This is the trigger half of the Emily ↔ Claude Code
// feedback loop documented in CLAUDE.md.
//
// Entity-graph observations (source == "entity-graph") receive a richer prompt
// that includes the current signal rules and instructs Claude to refine
// config/entity-graph-rules.json rather than Go source where possible.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// invokeWithRetry runs cmdName with args and retries if the output indicates a
// Claude API rate-limit or overload error. Retries up to 3 times with
// exponential back-off (30s → 90s → 270s). stderr is streamed live AND
// captured so rate-limit detection does not require disabling stderr output.
// After exhausting retries it fires a best-effort emily observe warning Apple.
//
// Context-too-long handling (AGI loop): if the output indicates the accumulated
// --continue session is too long, one recovery attempt is made without --continue
// so the session resets. This prevents the AGI loop from getting stuck in a
// permanent context-overflow snowball.
func invokeWithRetry(cmdName string, args []string) error {
	const maxRetries = 3
	const retryBase = 30 * time.Second
	var lastErr error
	contextResetDone := false
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			delay := retryBase * time.Duration(attempt)
			log.Printf("invoke retry %d/%d in %s (rate limit)", attempt, maxRetries, delay)
			time.Sleep(delay)
		}
		var buf bytes.Buffer
		cmd := exec.Command(cmdName, args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = io.MultiWriter(os.Stderr, &buf)
		if err := cmd.Run(); err != nil {
			lastErr = err
			output := buf.String()
			if isRateLimitOutput(output) {
				continue
			}
			if !contextResetDone && isContextTooLongOutput(output) {
				// AGI loop context overflow: drop --continue and retry fresh.
				args = dropFlag(args, "--continue")
				contextResetDone = true
				log.Printf("invoke: context too long — dropping --continue, retrying with fresh session")
				attempt-- // don't count this as a retry
				continue
			}
			return err
		}
		return nil
	}
	log.Printf("invoke: permanent failure after %d retries: %v", maxRetries, lastErr)
	_ = exec.Command("emily", "observe", "-s", "warning",
		"obs-watcher: claude invoke failed after retries",
		"--findings", lastErr.Error()).Run()
	return fmt.Errorf("invoke failed after %d retries: %w", maxRetries, lastErr)
}

// isContextTooLongOutput returns true when claude's output signals the prompt
// exceeds the model's context window (AGI --continue session overflow).
func isContextTooLongOutput(s string) bool {
	lower := strings.ToLower(s)
	return strings.Contains(lower, "prompt is too long") ||
		strings.Contains(lower, "context length") ||
		strings.Contains(lower, "context_length_exceeded") ||
		strings.Contains(lower, "maximum context") ||
		strings.Contains(lower, "too many tokens")
}

// dropFlag removes all occurrences of flag from args (exact string match).
func dropFlag(args []string, flag string) []string {
	out := args[:0:len(args)]
	for _, a := range args {
		if a != flag {
			out = append(out, a)
		}
	}
	return out
}

func isRateLimitOutput(s string) bool {
	lower := strings.ToLower(s)
	return strings.Contains(lower, "rate limit") ||
		strings.Contains(lower, "rate_limit") ||
		strings.Contains(lower, "429") ||
		strings.Contains(lower, "overloaded") ||
		strings.Contains(lower, "too many requests")
}

// resolveCmd finds the absolute path of a command. If cmdName contains a
// path separator it is returned unchanged. Otherwise exec.LookPath is tried
// first; if that fails, a set of common install locations is checked so that
// tools installed in ~/.local/bin (e.g. the `claude` CLI) are found even when
// that directory is not on the invoking process's PATH.
func resolveCmd(cmdName string) string {
	if strings.ContainsRune(cmdName, '/') {
		return cmdName
	}
	if resolved, err := exec.LookPath(cmdName); err == nil {
		return resolved
	}
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, ".local", "bin", cmdName),
		filepath.Join(home, "bin", cmdName),
		filepath.Join("/usr/local/bin", cmdName),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			log.Printf("cmd %q not on PATH; resolved via fallback: %s", cmdName, c)
			return c
		}
	}
	return cmdName
}

// observation is a union of Emily's generic health-sweep fields and the
// entity-graph structured fields. Fields absent in any given JSON file
// will be zero-valued and are excluded from the hash where appropriate.
type observation struct {
	// Generic Emily fields
	Timestamp    string `json:"timestamp"`
	Summary      string `json:"summary"`
	Severity     string `json:"severity"`
	Findings     string `json:"findings"`
	SuggestedFix string `json:"suggested_fix"`

	// Entity-graph fields (source == "entity-graph")
	Source           string         `json:"source,omitempty"`
	Status           string         `json:"status,omitempty"`
	Subject          string         `json:"subject,omitempty"`
	FilingsProcessed int            `json:"filings_processed,omitempty"`
	DirectorsFound   int            `json:"directors_found,omitempty"`
	SignalsGenerated int            `json:"signals_generated,omitempty"`
	SignalsByType    map[string]int `json:"signals_by_type,omitempty"`
	Gaps             []string       `json:"gaps,omitempty"`
	ParseErrors      []interface{}  `json:"parse_errors,omitempty"`
	HighSeverity     []interface{}  `json:"high_severity_signals,omitempty"`
	RequestForClaude string         `json:"request_for_claude,omitempty"`
}

func main() {
	var (
		root        = flag.String("root", envOr("FATBABY_ROOT", "."), "fatbaby project root")
		interval    = flag.Duration("interval", 10*time.Second, "poll interval")
		cmdName     = flag.String("cmd", envOr("OBSERVATION_CMD", "claude"), "command to invoke when a new observation arrives")
		extraArg    = flag.String("extra-args", envOr("OBSERVATION_CMD_ARGS", "--dangerously-skip-permissions"), "space-separated extra args passed to the command before the prompt")
		rulesPath   = flag.String("rules", envOr("ENTITY_GRAPH_RULES", ""), "path to entity-graph-rules.json; included in refinement prompts when non-empty")
		oneShot     = flag.Bool("one-shot", false, "process at most one observation, then exit")
		dryRun      = flag.Bool("dry-run", false, "log what would be invoked, do not actually run the command")
		gateMode    = flag.String("gate", envOr("OBSERVATION_GATE", "nontrivial"), "gate mode: 'none' (always invoke), 'nontrivial' (skip batches where only high_trust signals fired and no parse errors or gaps)")
		primeDir         = flag.String("prime-tasks", envOr("EMILY_PRIME_TASKS_DIR", ""), "path to Emily Prime signals/tasks/ directory; polls for directed tasks when set")
		batchWindow      = flag.Duration("batch-window", 60*time.Second, "when >0, collect all new observations within this window and invoke Claude once for the batch; set to 0 to process each observation individually")
		continueSession  = flag.Bool("continue", envOr("OBSERVATION_CONTINUE", "") == "true", "pass --continue to claude so each RSI cycle continues the prior session (AGI loop mode)")
	)
	flag.Parse()

	// AGI loop mode: append --continue so every claude invocation resumes the
	// most recent session from this directory, building context across RSI cycles.
	if *continueSession {
		*extraArg = strings.TrimSpace(*extraArg + " --continue")
		log.Printf("AGI loop mode: --continue enabled (sessions persist across RSI cycles)")
	}

	gh := newGithubClient(
		envOr("GITHUB_TOKEN", ""),
		envOr("GITHUB_OWNER", ""),
		envOr("GITHUB_REPO", ""),
	)
	if gh != nil {
		log.Printf("github issues enabled: %s/%s", gh.owner, gh.repo)
	}

	if *rulesPath == "" {
		*rulesPath = filepath.Join(*root, "config", "entity-graph-rules.json")
	}

	dir := filepath.Join(*root, "var", "emily-observations")
	latest := filepath.Join(dir, "latest.json")
	cursor := filepath.Join(dir, ".last-processed")
	batchCursor := filepath.Join(dir, ".last-batch-processed")

	// Auto-detect Emily Prime tasks dir if not explicitly configured.
	// Looks for the sibling EMILY/signals/tasks directory — the standard layout
	// when PRRJECT_FATBABY and EMILY share a parent directory.
	if *primeDir == "" {
		candidate := filepath.Join(*root, "..", "EMILY", "signals", "tasks")
		if _, err := os.Stat(candidate); err == nil {
			*primeDir = candidate
		}
	}

	// Prime task poller cursor — tracks last-processed task file.
	var primeTaskCursor string
	if *primeDir != "" {
		primeTaskCursor = filepath.Join(*primeDir, ".last-processed")
		log.Printf("prime tasks: watching %s", *primeDir)
	}

	// Resolve the command early so PATH issues are caught and logged at startup
	// rather than silently failing on first dispatch.
	*cmdName = resolveCmd(*cmdName)

	log.SetPrefix("observation-watcher ")
	log.Printf("watching %s (interval=%s cmd=%q dry_run=%v)", latest, *interval, *cmdName, *dryRun)

	for {
		var processed bool
		var pollErr error
		if *batchWindow > 0 {
			processed, pollErr = pollBatched(dir, batchCursor, *cmdName, *extraArg, *dryRun, *rulesPath, *gateMode, *batchWindow, gh)
		} else {
			processed, pollErr = pollOnce(latest, cursor, *cmdName, *extraArg, *dryRun, *rulesPath, *gateMode, gh)
		}
		if pollErr != nil {
			log.Printf("poll error: %v", pollErr)
		}

		// Poll Emily Prime's tasks directory if configured.
		if *primeDir != "" {
			if taskProcessed, taskErr := pollPrimeTasks(*primeDir, primeTaskCursor, *cmdName, *extraArg, *dryRun); taskErr != nil {
				log.Printf("prime task poll error: %v", taskErr)
			} else if taskProcessed {
				log.Printf("prime task processed")
			}
		}

		if *oneShot && processed {
			return
		}
		if *oneShot {
			return
		}
		time.Sleep(*interval)
	}
}

// ---------------------------------------------------------------------------
// Emily Prime task polling
// ---------------------------------------------------------------------------

// primeTask is the minimal structure needed to build a Claude prompt from a
// directed task issued by Emily Prime.
type primeTask struct {
	Timestamp          string   `json:"timestamp"`
	TaskID             string   `json:"task_id"`
	TaskType           string   `json:"task_type"`
	Priority           string   `json:"priority"`
	Description        string   `json:"description"`
	AcceptanceCriteria []string `json:"acceptance_criteria"`
	Context            string   `json:"context"`
	Deadline           string   `json:"deadline,omitempty"`
}

// primeDedupWindow is the minimum interval between Claude Code dispatches for
// identical prime tasks (same task_type + description). The rsi-loop.sh preset
// rotation fires the same preset every 30s; without this guard each firing
// queues an independent Claude Code session (~100K–200K tokens each).
const primeDedupWindow = 8 * time.Hour

// rsiReportDedupWindow is the dedup window for rsi_report tasks specifically.
// rsi-token-report presets run frequently; 8h prevents redundant analysis cycles
// that produce near-identical reports and consume ~150K tokens each.
const rsiReportDedupWindow = 8 * time.Hour

// dedupWindowFor returns the appropriate dedup window for the given task type.
func dedupWindowFor(taskType string) time.Duration {
	if taskType == "rsi_report" {
		return rsiReportDedupWindow
	}
	return primeDedupWindow
}

// primeTaskDuplicateExists returns true when another task file in tasksDir
// (excluding currentFile) has the same task_type and description and was
// written within lookback. Filenames are timestamp-prefixed so the cutoff
// is a simple lexicographic comparison after stripping colons.
func primeTaskDuplicateExists(tasksDir, currentFile, taskType, description string, lookback time.Duration) bool {
	cutoff := strings.ReplaceAll(
		time.Now().UTC().Add(-lookback).Format(time.RFC3339), ":", "")
	entries, err := os.ReadDir(tasksDir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if e.Name() == currentFile {
			continue // skip self
		}
		if e.Name() < cutoff {
			continue // outside dedup window
		}
		data, err := os.ReadFile(filepath.Join(tasksDir, e.Name()))
		if err != nil {
			continue
		}
		var t primeTask
		if err := json.Unmarshal(data, &t); err != nil {
			continue
		}
		if t.TaskType == taskType && t.Description == description {
			return true
		}
	}
	return false
}

// pollPrimeTasks checks the tasks directory for new task files and invokes
// Claude for each unprocessed one. Tasks are processed in filename order
// (which is timestamp order since filenames are timestamp-prefixed).
// Identical tasks (same type + description) within primeDedupWindow are
// skipped to prevent the rsi-loop.sh preset rotation from flooding Claude.
func pollPrimeTasks(tasksDir, cursorPath, cmdName, extraArgs string, dryRun bool) (bool, error) {
	entries, err := os.ReadDir(tasksDir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("read prime tasks dir: %w", err)
	}

	// Read last-processed task ID from cursor.
	lastProcessed := ""
	if b, err := os.ReadFile(cursorPath); err == nil {
		lastProcessed = strings.TrimSpace(string(b))
	}

	processed := false
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if lastProcessed != "" && e.Name() <= lastProcessed {
			continue
		}

		path := filepath.Join(tasksDir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			log.Printf("prime task read %s: %v", e.Name(), err)
			continue
		}
		var task primeTask
		if err := json.Unmarshal(data, &task); err != nil {
			log.Printf("prime task parse %s: %v", e.Name(), err)
			continue
		}

		// Dedup: skip identical task type+description seen within the dedup window.
		// Advances the cursor so the file is not re-evaluated on the next poll.
		dedupWindow := dedupWindowFor(task.TaskType)
		if primeTaskDuplicateExists(tasksDir, e.Name(), task.TaskType, task.Description, dedupWindow) {
			log.Printf("prime task dedup: skipping %s (type=%s dispatched within %s)", e.Name(), task.TaskType, dedupWindow)
			if err := os.WriteFile(cursorPath, []byte(e.Name()), 0o644); err != nil {
				return processed, fmt.Errorf("update prime task cursor (dedup skip): %w", err)
			}
			lastProcessed = e.Name()
			continue
		}

		log.Printf("new prime task task_id=%s type=%s priority=%s", task.TaskID, task.TaskType, task.Priority)

		prompt := buildPrimeTaskPrompt(path, task)
		if !dryRun {
			args := splitArgs(extraArgs)
			args = append(args, prompt)
			if err := invokeWithRetry(cmdName, args); err != nil {
				log.Printf("prime task invoke %s failed: %v", cmdName, err)
				continue
			}
		}

		if err := os.WriteFile(cursorPath, []byte(e.Name()), 0o644); err != nil {
			return processed, fmt.Errorf("update prime task cursor: %w", err)
		}
		lastProcessed = e.Name()
		processed = true
	}
	return processed, nil
}

func buildPrimeTaskPrompt(taskPath string, task primeTask) string {
	var sb strings.Builder
	sb.WriteString("Emily Prime has issued a directed task to FatBaby-Emily. Act on it.\n\n")
	fmt.Fprintf(&sb, "## Task (%s)\n", taskPath)
	fmt.Fprintf(&sb, "Task ID:   %s\n", task.TaskID)
	fmt.Fprintf(&sb, "Type:      %s\n", task.TaskType)
	fmt.Fprintf(&sb, "Priority:  %s\n", task.Priority)
	fmt.Fprintf(&sb, "Issued at: %s\n", task.Timestamp)
	if task.Deadline != "" {
		fmt.Fprintf(&sb, "Deadline:  %s\n", task.Deadline)
	}
	fmt.Fprintf(&sb, "\n## Description\n%s\n", task.Description)
	if task.Context != "" {
		fmt.Fprintf(&sb, "\n## Strategic context (from Emily Prime)\n%s\n", task.Context)
	}
	if len(task.AcceptanceCriteria) > 0 {
		sb.WriteString("\n## Acceptance criteria\n")
		for i, c := range task.AcceptanceCriteria {
			fmt.Fprintf(&sb, "%d. %s\n", i+1, c)
		}
	}
	sb.WriteString("\n## Your task\n")
	sb.WriteString("1. Understand what Emily Prime is asking for and why (see strategic context above).\n")
	sb.WriteString("2. Implement the requested change in the FatBaby codebase.\n")
	sb.WriteString("3. Run `go test ./...` before committing — fix any failures.\n")
	sb.WriteString("4. Commit with a message that references the task ID and describes the change.\n")
	sb.WriteString("5. Document the change in CHANGELOG.md.\n")
	sb.WriteString(runReportFooter(task.Description, task.Timestamp))
	return sb.String()
}

// runReportFooter returns the mandatory reporting and git-sync section that is
// appended to every prompt sent to Claude Code. It ensures every autonomous run
// produces an auditable JSON artifact, a pushed git commit, and an Apple.
func runReportFooter(summary, observationTimestamp string) string {
	return `
---
## MANDATORY END-OF-RUN STEPS (all required)

**1. CHANGELOG** — if any source file changed, prepend a dated bullet to CHANGELOG.md before committing.

**2. Run report** — write claude-runs/YYYY-MM-DDTHH:MM:SS.json (create dir if absent):
{"observation_summary":"` + summary + `","observation_timestamp":"` + observationTimestamp + `","run_started_at":"<ISO>","run_completed_at":"<ISO>","files_changed":["<path>"],"actions_taken":"<summary>","git_commit_hash":"<hash>","exit_status":"success|partial|failed","tokens_used":<int>,"notes":"<caveats>"}

**3. Git sync** — include the run report in the commit:
  git add -A
  git commit -m "claude-code: ` + summary + ` [` + observationTimestamp + `]"
  git push
Retry push once on failure; record failure in notes, set exit_status "partial". Never skip.

**4. Apple** — file completion to IDUNA:
  emily apples post -t completion -repo PRRJECT_FATBABY "` + summary + `"
If IDUNA offline, record in notes, set exit_status "partial".

**5. Backlog** — if this addresses EMILY/BACKLOG.md item, mark [x] with Apple ID + date, then:
  cd /home/fatbaby/EMILY && git add BACKLOG.md && git commit -m "backlog: ✓ <item>" && git push

**6. Golden docs** — if you created a NORTHSTAR.md or mission-critical spec, add a row to
EMILY/context/golden-docs-index.md and commit+push EMILY.
`
}

// exec is already imported above; re-declare only if needed.

// observationHash returns a stable hash of the observation's meaningful
// content, excluding timestamp, so Claude is only re-triggered when findings
// actually change (not on every Emily tick).
func observationHash(o observation) string {
	h := sha256.New()
	// Generic Emily fields
	fmt.Fprintf(h, "severity=%s\nsummary=%s\nfindings=%s\nsuggested_fix=%s\n",
		o.Severity, o.Summary, o.Findings, o.SuggestedFix)
	// Entity-graph fields (zero for generic observations)
	fmt.Fprintf(h, "source=%s\nstatus=%s\nsubject=%s\nrequest=%s\n",
		o.Source, o.Status, o.Subject, o.RequestForClaude)
	fmt.Fprintf(h, "filings=%d\ndirectors=%d\nsignals=%d\n",
		o.FilingsProcessed, o.DirectorsFound, o.SignalsGenerated)
	fmt.Fprintf(h, "gaps=%v\n", o.Gaps)
	return hex.EncodeToString(h.Sum(nil))
}

// isTrivialObservation returns true when the observation carries no actionable
// content: status is "ok", no parse errors, no gaps, and every signal that fired
// is a high_trust_director (informational-only; no refinement needed).
//
// Generic Emily observations (no source/status fields) use severity to signal
// urgency; any non-empty, non-"ok" severity is nontrivial regardless of the
// entity-graph fields. Observations with explicit Findings or SuggestedFix text
// are always nontrivial — Emily wrote them to prompt action.
func isTrivialObservation(obs observation) bool {
	// Explicit findings or a suggested fix means Emily identified something actionable.
	if obs.Findings != "" || obs.SuggestedFix != "" {
		return false
	}
	if obs.Severity != "" && obs.Severity != "ok" && obs.Severity != "info" {
		return false
	}
	if obs.Status == "needs_attention" {
		return false
	}
	if len(obs.ParseErrors) > 0 || len(obs.Gaps) > 0 {
		return false
	}
	for t, count := range obs.SignalsByType {
		if count > 0 && t != "high_trust_director" {
			return false
		}
	}
	return true
}

// pollOnce checks latest.json and, if its content hash differs from the cursor,
// invokes the configured command and updates the cursor. Returns true if an
// observation was processed.
func pollOnce(latestPath, cursorPath, cmdName, extraArgs string, dryRun bool, rulesPath, gateMode string, gh *githubClient) (bool, error) {
	b, err := os.ReadFile(latestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("read latest: %w", err)
	}
	var obs observation
	if err := json.Unmarshal(b, &obs); err != nil {
		return false, fmt.Errorf("parse latest: %w", err)
	}
	if obs.Timestamp == "" {
		return false, fmt.Errorf("observation missing timestamp")
	}
	hash := observationHash(obs)
	last, _ := os.ReadFile(cursorPath)
	if strings.TrimSpace(string(last)) == hash {
		return false, nil
	}

	subject := obs.Summary
	if obs.Subject != "" {
		subject = obs.Subject
	}
	log.Printf("new observation timestamp=%s source=%s status=%s subject=%q hash=%s", obs.Timestamp, obs.Source, obs.Status, subject, hash[:12])

	// Gate: skip low-information observations to avoid burning Claude API quota.
	if gateMode == "nontrivial" && isTrivialObservation(obs) {
		log.Printf("gate=nontrivial skipping trivial observation (only high_trust signals, no gaps or errors) — updating cursor without invoking claude")
		if err := os.WriteFile(cursorPath, []byte(hash), 0o644); err != nil {
			return false, fmt.Errorf("update cursor: %w", err)
		}
		return true, nil
	}

	title := obs.Summary
	if obs.Subject != "" {
		title = obs.Subject
	}
	if gh != nil && !dryRun {
		issueURL, issueErr := gh.createIssue(
			"[Emily observation] "+title,
			issueBodyForObservation(obs, latestPath),
		)
		if issueErr != nil {
			log.Printf("github issue create failed: %v", issueErr)
		} else if issueURL != "" {
			log.Printf("github issue created: %s", issueURL)
		}
	}

	prompt := buildPrompt(latestPath, obs, rulesPath)

	if !dryRun {
		args := splitArgs(extraArgs)
		args = append(args, prompt)
		if err := invokeWithRetry(cmdName, args); err != nil {
			return false, fmt.Errorf("invoke %s: %w", cmdName, err)
		}
	}

	if err := os.WriteFile(cursorPath, []byte(hash), 0o644); err != nil {
		return false, fmt.Errorf("update cursor: %w", err)
	}
	return true, nil
}

// buildPrompt constructs the Claude prompt. Entity-graph observations receive a
// structured refinement prompt that includes the current rules file; generic
// Emily observations receive the simpler health-sweep prompt.
func buildPrompt(latestPath string, obs observation, rulesPath string) string {
	if obs.Source == "entity-graph" || obs.Subject != "" {
		return buildEntityGraphPrompt(latestPath, obs, rulesPath)
	}
	return buildGenericPrompt(latestPath, obs)
}

func buildGenericPrompt(latestPath string, obs observation) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Emily has published a %s-severity observation. Read %s for full context, then act.\n\n", obs.Severity, latestPath)
	fmt.Fprintf(&sb, "Summary: %s\n\n", obs.Summary)
	if obs.SuggestedFix != "" {
		fmt.Fprintf(&sb, "Suggested fix (from Emily):\n%s\n\n", obs.SuggestedFix)
	}
	if obs.Findings != "" {
		fmt.Fprintf(&sb, "Findings:\n%s\n\n", obs.Findings)
	}
	sb.WriteString("Instructions:\n")
	sb.WriteString("1. Read the full observation file for any context not shown above.\n")
	sb.WriteString("2. Identify the root cause in the source code.\n")
	sb.WriteString("3. If the suggested_fix is sound, implement it exactly. If not, implement the correct fix and note the deviation.\n")
	sb.WriteString("4. Run `go test ./...` before committing — fix any test failures.\n")
	sb.WriteString("5. Commit with a message describing the fix and its root cause.\n")
	sb.WriteString("6. Document the change in CHANGELOG.md with today's date.\n")
	sb.WriteString(runReportFooter(obs.Summary, obs.Timestamp))
	return sb.String()
}

func buildEntityGraphPrompt(latestPath string, obs observation, rulesPath string) string {
	var sb strings.Builder

	sb.WriteString("You are acting as the NORTHSTAR recursive self-improvement engine for the entity-graph intelligence pipeline. ")
	sb.WriteString("An observation has been published from the entity-graph process. Your job is to refine the signal rules or parser based on what you see.\n\n")

	fmt.Fprintf(&sb, "## Observation (%s)\n", latestPath)
	fmt.Fprintf(&sb, "Source:            %s\n", obs.Source)
	fmt.Fprintf(&sb, "Status:            %s\n", obs.Status)
	fmt.Fprintf(&sb, "Subject:           %s\n", obs.Subject)
	fmt.Fprintf(&sb, "Filings processed: %d\n", obs.FilingsProcessed)
	fmt.Fprintf(&sb, "Directors found:   %d\n", obs.DirectorsFound)
	fmt.Fprintf(&sb, "Signals generated: %d\n", obs.SignalsGenerated)

	if len(obs.SignalsByType) > 0 {
		sb.WriteString("Signals by type:\n")
		for k, v := range obs.SignalsByType {
			fmt.Fprintf(&sb, "  %s: %d\n", k, v)
		}
	}

	if len(obs.Gaps) > 0 {
		sb.WriteString("Gaps detected:\n")
		for _, g := range obs.Gaps {
			fmt.Fprintf(&sb, "  - %s\n", g)
		}
	}

	if len(obs.ParseErrors) > 0 {
		fmt.Fprintf(&sb, "Parse errors: %d (see %s for details)\n", len(obs.ParseErrors), latestPath)
	}

	if obs.RequestForClaude != "" {
		fmt.Fprintf(&sb, "\n## Request from entity-graph\n%s\n", obs.RequestForClaude)
	}

	// Only inline rules when Claude actually needs to edit them: gaps present,
	// parse errors detected, or an explicit rule-change request. Clean-run
	// dispatches (signals fired, no parse failures) don't need the rules
	// payload, saving ~320T per invocation.
	needsRules := len(obs.Gaps) > 0 || len(obs.ParseErrors) > 0 ||
		strings.Contains(strings.ToLower(obs.RequestForClaude), "rule") ||
		strings.Contains(strings.ToLower(obs.RequestForClaude), "threshold")
	if needsRules && rulesPath != "" {
		if rulesJSON, err := os.ReadFile(rulesPath); err == nil {
			fmt.Fprintf(&sb, "\n## Current Signal Rules (%s)\n```json\n%s\n```\n", rulesPath, rulesJSON)
		}
	}

	sb.WriteString("\n## Your task\n")
	sb.WriteString("1. Analyze the gaps and parse errors above.\n")
	sb.WriteString("2. If signal thresholds need adjustment, edit `config/entity-graph-rules.json` directly — do NOT touch Go source for threshold changes.\n")
	sb.WriteString("3. If parse errors are structural (regex misses, form variants), edit `internal/entitygraph/parser.go` and add a test case to `internal/entitygraph/parser_test.go`.\n")
	sb.WriteString("4. Run `go test ./...` to verify all changes.\n")
	sb.WriteString("5. Commit passing changes with a descriptive message explaining the refinement.\n")
	sb.WriteString("6. Document the change in CHANGELOG.md with today's date.\n")
	sb.WriteString(runReportFooter(obs.RequestForClaude, obs.Timestamp))

	return sb.String()
}

// pollBatched scans the observations directory for all .json files newer than
// the batch cursor (last-processed filename) and invokes Claude once for the
// entire batch. Files within batchWindow of each other are treated as one batch.
// Trivial observations are filtered out by the gate mode before batching.
// Returns true if at least one non-trivial batch was dispatched.
func pollBatched(dir, cursorPath, cmdName, extraArgs string, dryRun bool, rulesPath, gateMode string, batchWindow time.Duration, gh *githubClient) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("read obs dir: %w", err)
	}

	lastProcessed := ""
	if b, err := os.ReadFile(cursorPath); err == nil {
		lastProcessed = strings.TrimSpace(string(b))
	}

	// Collect all new observation files in filename order (timestamp-sorted).
	type candidate struct {
		name string
		path string
		obs  observation
	}
	var candidates []candidate
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		// Skip symlinks (e.g. latest.json → timestamped file). The target file is
		// already processed directly; including the symlink would re-trigger on every
		// poll because "latest.json" sorts after any "2026…" cursor value.
		if e.Type()&os.ModeSymlink != 0 {
			continue
		}
		if lastProcessed != "" && e.Name() <= lastProcessed {
			continue
		}
		p := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var obs observation
		if err := json.Unmarshal(data, &obs); err != nil || obs.Timestamp == "" {
			continue
		}
		candidates = append(candidates, candidate{e.Name(), p, obs})
	}
	if len(candidates) == 0 {
		return false, nil
	}

	// Apply gate: filter trivial observations before batching.
	var nontrivial []candidate
	for _, c := range candidates {
		if gateMode == "nontrivial" && isTrivialObservation(c.obs) {
			log.Printf("batch gate: skipping trivial %s", c.name)
		} else {
			nontrivial = append(nontrivial, c)
		}
	}
	// Always advance the cursor to the last candidate so we don't re-process trivials.
	newestName := candidates[len(candidates)-1].name

	if len(nontrivial) == 0 {
		log.Printf("batch: %d new obs, all trivial — skipping invocation, advancing cursor", len(candidates))
		if err := os.WriteFile(cursorPath, []byte(newestName), 0o644); err != nil {
			return false, fmt.Errorf("update batch cursor: %w", err)
		}
		return true, nil
	}

	log.Printf("batch: %d new obs (%d nontrivial) — invoking Claude once for the batch", len(candidates), len(nontrivial))

	var obsList []observation
	var paths []string
	for _, c := range nontrivial {
		obsList = append(obsList, c.obs)
		paths = append(paths, c.path)
		if gh != nil && !dryRun {
			title := c.obs.Summary
			if c.obs.Subject != "" {
				title = c.obs.Subject
			}
			issueURL, issueErr := gh.createIssue(
				"[Emily observation] "+title,
				issueBodyForObservation(c.obs, c.path),
			)
			if issueErr != nil {
				log.Printf("github issue create failed: %v", issueErr)
			} else if issueURL != "" {
				log.Printf("github issue created: %s", issueURL)
			}
		}
	}
	prompt := buildBatchedPrompt(obsList, paths, rulesPath, batchWindow)

	if !dryRun {
		args := splitArgs(extraArgs)
		args = append(args, prompt)
		if err := invokeWithRetry(cmdName, args); err != nil {
			return false, fmt.Errorf("invoke %s: %w", cmdName, err)
		}
	}

	if err := os.WriteFile(cursorPath, []byte(newestName), 0o644); err != nil {
		return false, fmt.Errorf("update batch cursor: %w", err)
	}
	return true, nil
}

// buildBatchedPrompt builds a single Claude prompt summarising all observations
// in the batch. Entity-graph observations are handled inline with their details.
func buildBatchedPrompt(observations []observation, paths []string, rulesPath string, batchWindow time.Duration) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Emily has published %d observations since the last Claude Code run (batch window: %s). Act on all of them in a single pass to minimise token overhead.\n\n", len(observations), batchWindow)

	for i, obs := range observations {
		fmt.Fprintf(&sb, "---\n## Observation %d of %d — %s\n", i+1, len(observations), obs.Timestamp)
		if obs.Source != "" {
			fmt.Fprintf(&sb, "Source: %s | Status: %s | Subject: %s\n", obs.Source, obs.Status, obs.Subject)
			fmt.Fprintf(&sb, "Filings: %d | Directors: %d | Signals: %d\n", obs.FilingsProcessed, obs.DirectorsFound, obs.SignalsGenerated)
			if len(obs.Gaps) > 0 {
				fmt.Fprintf(&sb, "Gaps: %v\n", obs.Gaps)
			}
			if len(obs.ParseErrors) > 0 {
				fmt.Fprintf(&sb, "Parse errors: %d (see %s)\n", len(obs.ParseErrors), paths[i])
			}
			if obs.RequestForClaude != "" {
				fmt.Fprintf(&sb, "Request: %s\n", obs.RequestForClaude)
			}
		} else {
			fmt.Fprintf(&sb, "Severity: %s\nSummary: %s\n", obs.Severity, obs.Summary)
			if obs.Findings != "" {
				fmt.Fprintf(&sb, "Findings: %s\n", obs.Findings)
			}
			if obs.SuggestedFix != "" {
				fmt.Fprintf(&sb, "Suggested fix: %s\n", obs.SuggestedFix)
			}
		}
	}

	// Inline rules only when at least one entity-graph observation has actionable
	// gaps, parse errors, or an explicit rule-change request. Saves ~320T per
	// batch invocation when all entity-graph entries are clean-run reports.
	needsRules := false
	for _, obs := range observations {
		if obs.Source == "entity-graph" || obs.Subject != "" {
			if len(obs.Gaps) > 0 || len(obs.ParseErrors) > 0 ||
				strings.Contains(strings.ToLower(obs.RequestForClaude), "rule") ||
				strings.Contains(strings.ToLower(obs.RequestForClaude), "threshold") {
				needsRules = true
				break
			}
		}
	}
	if needsRules && rulesPath != "" {
		if rulesJSON, err := os.ReadFile(rulesPath); err == nil {
			fmt.Fprintf(&sb, "\n---\n## Current Signal Rules (%s)\n```json\n%s\n```\n", rulesPath, rulesJSON)
		}
	}

	sb.WriteString("\n---\n## Your task (batched)\n")
	sb.WriteString("Process all observations above in priority order (gaps and parse errors first).\n")
	sb.WriteString("1. For each observation that requires action: identify root cause, implement fix, run `go test ./...`.\n")
	sb.WriteString("2. Group related fixes into as few commits as possible.\n")
	sb.WriteString("3. Document all changes in CHANGELOG.md with today's date.\n")

	// Use the last observation's summary/timestamp for the run report footer.
	last := observations[len(observations)-1]
	batchSummary := fmt.Sprintf("batched %d observations ending at %s", len(observations), last.Timestamp)
	sb.WriteString(runReportFooter(batchSummary, last.Timestamp))
	return sb.String()
}

func splitArgs(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return strings.Fields(s)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
