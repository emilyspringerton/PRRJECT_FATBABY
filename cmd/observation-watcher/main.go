// observation-watcher polls var/emily-observations/latest.json and, when a
// new observation appears (distinguished by its timestamp field), invokes
// the configured command — typically `claude` — with a prompt that tells
// it to act on the observation. This is the trigger half of the Emily ↔
// Claude Code feedback loop documented in CLAUDE.md.
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

type observation struct {
	Timestamp    string `json:"timestamp"`
	Summary      string `json:"summary"`
	Severity     string `json:"severity"`
	Findings     string `json:"findings"`
	SuggestedFix string `json:"suggested_fix"`
}

func main() {
	var (
		root     = flag.String("root", envOr("FATBABY_ROOT", "."), "fatbaby project root")
		interval = flag.Duration("interval", 10*time.Second, "poll interval")
		cmdName  = flag.String("cmd", envOr("OBSERVATION_CMD", "claude"), "command to invoke when a new observation arrives")
		extraArg = flag.String("extra-args", envOr("OBSERVATION_CMD_ARGS", "--dangerously-skip-permissions"), "space-separated extra args passed to the command before the prompt")
		oneShot  = flag.Bool("one-shot", false, "process at most one observation, then exit")
		dryRun   = flag.Bool("dry-run", false, "log what would be invoked, do not actually run the command")
	)
	flag.Parse()

	dir := filepath.Join(*root, "var", "emily-observations")
	latest := filepath.Join(dir, "latest.json")
	cursor := filepath.Join(dir, ".last-processed")

	log.SetPrefix("observation-watcher ")
	log.Printf("watching %s (interval=%s cmd=%q dry_run=%v)", latest, *interval, *cmdName, *dryRun)

	for {
		processed, err := pollOnce(latest, cursor, *cmdName, *extraArg, *dryRun)
		if err != nil {
			log.Printf("poll error: %v", err)
		}
		if *oneShot && processed {
			return
		}
		if *oneShot {
			// Nothing to process; still exit so callers (cron, CI) can re-run later.
			return
		}
		time.Sleep(*interval)
	}
}

// observationHash captures the content of an observation that, when changed,
// represents a genuinely new finding worth re-triggering Claude Code over.
// Timestamp is excluded so Emily can re-publish the same finding without
// causing spurious re-runs.
func observationHash(o observation) string {
	h := sha256.New()
	fmt.Fprintf(h, "severity=%s\nsummary=%s\nfindings=%s\nsuggested_fix=%s\n",
		o.Severity, o.Summary, o.Findings, o.SuggestedFix)
	return hex.EncodeToString(h.Sum(nil))
}

// pollOnce checks latest.json, and if its content hash differs from the
// cursor, invokes the configured command and updates the cursor. Returns
// true if an observation was processed.
func pollOnce(latestPath, cursorPath, cmdName, extraArgs string, dryRun bool) (bool, error) {
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

	prompt := buildPrompt(latestPath, obs)
	log.Printf("new observation timestamp=%s severity=%s summary=%q hash=%s", obs.Timestamp, obs.Severity, obs.Summary, hash[:12])

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

func buildPrompt(latestPath string, obs observation) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Read %s and act on Emily's observation. ", latestPath)
	fmt.Fprintf(&sb, "Summary: %s. Severity: %s. ", obs.Summary, obs.Severity)
	sb.WriteString("Run `go test ./...` before committing. ")
	sb.WriteString("If the suggested_fix is sound, implement it; otherwise, propose an alternative and document it in CHANGELOG.md.")
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
