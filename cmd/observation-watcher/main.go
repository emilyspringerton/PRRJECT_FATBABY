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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

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
		root      = flag.String("root", envOr("FATBABY_ROOT", "."), "fatbaby project root")
		interval  = flag.Duration("interval", 10*time.Second, "poll interval")
		cmdName   = flag.String("cmd", envOr("OBSERVATION_CMD", "claude"), "command to invoke when a new observation arrives")
		extraArg  = flag.String("extra-args", envOr("OBSERVATION_CMD_ARGS", "--dangerously-skip-permissions"), "space-separated extra args passed to the command before the prompt")
		rulesPath = flag.String("rules", envOr("ENTITY_GRAPH_RULES", ""), "path to entity-graph-rules.json; included in refinement prompts when non-empty")
		oneShot   = flag.Bool("one-shot", false, "process at most one observation, then exit")
		dryRun    = flag.Bool("dry-run", false, "log what would be invoked, do not actually run the command")
	)
	flag.Parse()

	if *rulesPath == "" {
		*rulesPath = filepath.Join(*root, "config", "entity-graph-rules.json")
	}

	dir := filepath.Join(*root, "var", "emily-observations")
	latest := filepath.Join(dir, "latest.json")
	cursor := filepath.Join(dir, ".last-processed")

	log.SetPrefix("observation-watcher ")
	log.Printf("watching %s (interval=%s cmd=%q dry_run=%v)", latest, *interval, *cmdName, *dryRun)

	for {
		processed, err := pollOnce(latest, cursor, *cmdName, *extraArg, *dryRun, *rulesPath)
		if err != nil {
			log.Printf("poll error: %v", err)
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

// pollOnce checks latest.json and, if its content hash differs from the cursor,
// invokes the configured command and updates the cursor. Returns true if an
// observation was processed.
func pollOnce(latestPath, cursorPath, cmdName, extraArgs string, dryRun bool, rulesPath string) (bool, error) {
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

	prompt := buildPrompt(latestPath, obs, rulesPath)
	subject := obs.Summary
	if obs.Subject != "" {
		subject = obs.Subject
	}
	log.Printf("new observation timestamp=%s source=%s status=%s subject=%q hash=%s", obs.Timestamp, obs.Source, obs.Status, subject, hash[:12])

	if !dryRun {
		args := splitArgs(extraArgs)
		args = append(args, prompt)
		cmd := exec.Command(cmdName, args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
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
	fmt.Fprintf(&sb, "Read %s and act on Emily's observation. ", latestPath)
	fmt.Fprintf(&sb, "Summary: %s. Severity: %s. ", obs.Summary, obs.Severity)
	sb.WriteString("Run `go test ./...` before committing. ")
	sb.WriteString("If the suggested_fix is sound, implement it; otherwise, propose an alternative and document it in CHANGELOG.md.")
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

	// Inline current rules so Claude can propose concrete edits.
	if rulesPath != "" {
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
