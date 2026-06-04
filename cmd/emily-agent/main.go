package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/example/prrject-fatbaby/internal/iamguard"
	"github.com/example/prrject-fatbaby/pkg/intelligence"
)

// Config holds emily-agent runtime configuration.
type Config struct {
	Port            string
	ConversationDir string
	Model           string
	ValidatorModel  string
	APIKey          string // Anthropic API key.
	GitCommit       bool
	RateLimitRPM    int
	MaxToolIters    int
	FatbabyRoot     string
}

type rateLimiter struct{ ch <-chan time.Time }

func newRateLimiter(rpm int) *rateLimiter {
	if rpm <= 0 {
		rpm = 20
	}
	interval := time.Minute / time.Duration(rpm)
	if interval <= 0 {
		interval = time.Second
	}
	t := time.NewTicker(interval)
	return &rateLimiter{ch: t.C}
}
func (r *rateLimiter) Wait() { <-r.ch }

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func loadConfig() Config {
	gitCommit := true
	if v := os.Getenv("GIT_COMMIT"); v == "false" {
		gitCommit = false
	}
	rpm, _ := strconv.Atoi(envOr("RATE_LIMIT_RPM", "20"))
	maxIters, _ := strconv.Atoi(envOr("MAX_TOOL_ITERS", "10"))
	if maxIters <= 0 {
		maxIters = 10
	}
	if rpm <= 0 {
		rpm = 20
	}
	return Config{Port: envOr("PORT", "8080"), ConversationDir: envOr("CONVERSATION_DIR", "./conversations"), Model: envOr("MODEL", "gpt-4o-mini"), ValidatorModel: envOr("VALIDATOR_MODEL", "gpt-4o-mini"), APIKey: os.Getenv("ANTHROPIC_API_KEY"), GitCommit: gitCommit, RateLimitRPM: rpm, MaxToolIters: maxIters, FatbabyRoot: envOr("FATBABY_ROOT", ".")}
}

type ToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  ToolParameters `json:"parameters"`
}

type ToolParameters struct {
	Type       string                    `json:"type"`
	Properties map[string]ToolPropSchema `json:"properties"`
	Required   []string                  `json:"required,omitempty"`
}

type ToolPropSchema struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

type ToolFunc func(args map[string]any) (string, error)

type ToolDispatcher struct {
	defs     []ToolDef
	handlers map[string]ToolFunc
}

func NewToolDispatcher() *ToolDispatcher { return &ToolDispatcher{handlers: map[string]ToolFunc{}} }
func (d *ToolDispatcher) Register(def ToolDef, fn ToolFunc) {
	d.defs = append(d.defs, def)
	d.handlers[def.Name] = fn
}
func (d *ToolDispatcher) Defs() []ToolDef { return d.defs }
func (d *ToolDispatcher) AnthropicDefs() []map[string]any {
	out := make([]map[string]any, 0, len(d.defs))
	for _, td := range d.defs {
		props := map[string]any{}
		for k, v := range td.Parameters.Properties {
			props[k] = map[string]any{"type": v.Type, "description": v.Description}
		}
		out = append(out, map[string]any{"name": td.Name, "description": td.Description, "input_schema": map[string]any{"type": td.Parameters.Type, "properties": props, "required": td.Parameters.Required}})
	}
	return out
}

func registerGitTools(d *ToolDispatcher, repoDir string) {}
func absStorePath(root, store string) string             { return filepath.Join(root, "var", store) }

func openStoreOrMessage(root, storeName string) (*eventstore.FileStore, string, error) {
	dir := absStorePath(root, storeName)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil, "store not initialised — has the process been run?", nil
	}
	fs, err := eventstore.NewFileStore(dir)
	if err != nil {
		return nil, "", err
	}
	return fs, "", nil
}

func runCommandWithTimeout(args []string, workDir string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = workDir
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if len(text) > 4000 {
		text = text[:4000]
	}
	return text, err
}

func slugifySimple(s string, maxLen int) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else if b.Len() > 0 {
			b.WriteRune('-')
		}
		if b.Len() >= maxLen {
			break
		}
	}
	return strings.TrimRight(b.String(), "-")
}

// createGitHubIssue creates a GitHub issue for the given observation.
// Requires GITHUB_TOKEN, GITHUB_OWNER, GITHUB_REPO env vars.
// Fails silently (logs warning) when not configured so it never blocks observation writes.
func createGitHubIssue(summary, findings, suggestedFix, severity string) {
	token := os.Getenv("GITHUB_TOKEN")
	owner := os.Getenv("GITHUB_OWNER")
	repo := os.Getenv("GITHUB_REPO")
	if token == "" || owner == "" || repo == "" {
		return
	}

	severityLabel := map[string]string{
		"info":     "enhancement",
		"warn":     "bug",
		"error":    "critical",
		"critical": "critical",
	}[severity]
	if severityLabel == "" {
		severityLabel = "enhancement"
	}

	body := findings
	if suggestedFix != "" {
		body += "\n\n---\n**Suggested Fix**\n\n" + suggestedFix
	}

	payload := map[string]any{
		"title":  summary,
		"body":   body,
		"labels": []string{"emily-observation", severityLabel},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		log.Printf("github issue: marshal err: %v", err)
		return
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/issues", owner, repo)
	req, err := http.NewRequest("POST", url, bytes.NewReader(data))
	if err != nil {
		log.Printf("github issue: new request err: %v", err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req = req.WithContext(ctx)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("github issue: request err: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		log.Printf("github issue: status=%d body=%s", resp.StatusCode, raw)
		return
	}
	var result struct {
		Number int    `json:"number"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err == nil {
		log.Printf("github issue: created #%d %s", result.Number, result.HTMLURL)
	}
}

func parseProcOutput(s string) string {
	f := strings.Fields(strings.TrimSpace(s))
	if len(f) == 0 {
		return ""
	}
	return f[0]
}

func registerFatbabyTools(d *ToolDispatcher, fatbabyRoot string) {
	valid := map[string][]string{
		"secwatch":            {"-watchlist", "./config/watchlist.json", "-store", "./var/secwatch", "-poll-interval", "5m"},
		"processor":           {"-store", "./var/secwatch", "-workers", "4", "-poll-interval", "15s"},
		"newssite":            {"-store", "./var/secwatch", "-addr", ":8082"},
		"dashboard":           {"-data-dir", "./var/secwatch", "-port", "8080"},
		"prwatch":             {"-store", "./var/prwatch", "-poll-interval", "30s"},
		"prwatch-body":        {"-discovery-store", "./var/prwatch", "-body-store", "./var/prwatch-body", "-workers", "4", "-poll-interval", "15s"},
		"entity-graph":        {"-store", "./var/secwatch", "-graph-dir", "./var/entity-graph", "-obs-dir", "./var/emily-observations", "-rules", "./config/entity-graph-rules.json"},
		"eps-processor":       {"-discovery-store", "./var/prwatch", "-body-store", "./var/prwatch-body", "-eps-dir", "./var/eps"},
		"eps-reconciler":      {"-store", "./var/secwatch", "-eps-dir", "./var/eps"},
		"schd13-watcher":      {"-watchlist", "./config/watchlist.json", "-graph-dir", "./var/entity-graph", "-out-dir", "./var/schd13"},
		"observation-watcher": {},
		"signalapi":           {"-store", "./var/secwatch"},
		"feedserver":          {"-store", "./var/secwatch"},
		"jon-agent":           {},
		"form4-watcher":    {"-watchlist", "./config/watchlist.json", "-graph-dir", "./var/entity-graph", "-out-dir", "./var/form4"},
		"dividend-watcher": {"-discovery-store", "./var/prwatch", "-body-store", "./var/prwatch-body", "-graph-dir", "./var/entity-graph", "-out-dir", "./var/dividends"},
	}

	d.Register(ToolDef{Name: "fatbaby_start_process", Description: "Start a fatbaby pipeline process in the background.", Parameters: ToolParameters{Type: "object", Properties: map[string]ToolPropSchema{"process_name": {Type: "string", Description: "secwatch|processor|newssite|dashboard|prwatch|prwatch-body|entity-graph|eps-processor|eps-reconciler|schd13-watcher|observation-watcher|signalapi|feedserver|jon-agent|form4-watcher|dividend-watcher"}, "extra_args": {Type: "string", Description: "optional extra CLI args"}}, Required: []string{"process_name"}}}, func(args map[string]any) (string, error) {
		pn, _ := args["process_name"].(string)
		ea, _ := args["extra_args"].(string)
		def, ok := valid[pn]
		if !ok {
			return "", errors.New("invalid process_name")
		}
		res, _ := runCommandWithTimeout([]string{"pgrep", "-af", "cmd/" + pn}, fatbabyRoot)
		if pid := parseProcOutput(res); pid != "" {
			return "already running pid=" + pid, nil
		}
		if err := os.MkdirAll(filepath.Join(fatbabyRoot, "var", "logs"), 0o755); err != nil {
			return "", err
		}
		logPath := filepath.Join(fatbabyRoot, "var", "logs", pn+".log")
		f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return "", err
		}
		argv := append([]string{"run", "./cmd/" + pn}, def...)
		if strings.TrimSpace(ea) != "" {
			argv = append(argv, strings.Fields(ea)...)
		}
		cmd := exec.Command("go", argv...)
		cmd.Dir = fatbabyRoot
		cmd.Stdout = f
		cmd.Stderr = f
		if err := cmd.Start(); err != nil {
			return "", err
		}
		return fmt.Sprintf("started pid=%d log=var/logs/%s.log", cmd.Process.Pid, pn), nil
	})

	d.Register(ToolDef{Name: "fatbaby_stop_process", Description: "Stop a running fatbaby pipeline process by sending SIGTERM.", Parameters: ToolParameters{Type: "object", Properties: map[string]ToolPropSchema{"process_name": {Type: "string", Description: "process name"}}, Required: []string{"process_name"}}}, func(args map[string]any) (string, error) {
		pn, _ := args["process_name"].(string)
		out, _ := runCommandWithTimeout([]string{"pgrep", "-af", "cmd/" + pn}, fatbabyRoot)
		pid := parseProcOutput(out)
		if pid == "" {
			return "not running", nil
		}
		_, err := runCommandWithTimeout([]string{"kill", "-TERM", pid}, fatbabyRoot)
		if err != nil {
			return "", err
		}
		return "sent SIGTERM to pid=" + pid, nil
	})

	d.Register(ToolDef{Name: "fatbaby_read_log", Description: "Read the last N lines of a process log file.", Parameters: ToolParameters{Type: "object", Properties: map[string]ToolPropSchema{"process_name": {Type: "string", Description: "process name"}, "lines": {Type: "number", Description: "default 50"}}, Required: []string{"process_name"}}}, func(args map[string]any) (string, error) {
		pn, _ := args["process_name"].(string)
		n := 50
		if v, ok := args["lines"].(float64); ok && int(v) > 0 {
			n = int(v)
		}
		p := filepath.Join(fatbabyRoot, "var", "logs", pn+".log")
		b, err := os.ReadFile(p)
		if err != nil {
			if os.IsNotExist(err) {
				return "log not found — has this process been started by Emily?", nil
			}
			return "", err
		}
		lines := strings.Split(string(b), "\n")
		if len(lines) > n {
			lines = lines[len(lines)-n:]
		}
		out := strings.Join(lines, "\n")
		if len(out) > 3000 {
			out = "[truncated] " + out[len(out)-3000:]
		}
		return out, nil
	})

	d.Register(ToolDef{Name: "fatbaby_run_secwatch_once", Description: "Run one real SEC discovery pass and wait for completion.", Parameters: ToolParameters{Type: "object", Properties: map[string]ToolPropSchema{}}}, func(args map[string]any) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "go", "run", "./cmd/secwatch", "-watchlist", "./config/watchlist.json", "-store", "./var/secwatch")
		cmd.Dir = fatbabyRoot
		out, err := cmd.CombinedOutput()
		s := strings.TrimSpace(string(out))
		if len(s) > 8000 {
			s = s[:8000]
		}
		return s, err
	})

	d.Register(ToolDef{Name: "fatbaby_count_source_documents", Description: "Count source_document_persisted events in secwatch.", Parameters: ToolParameters{Type: "object", Properties: map[string]ToolPropSchema{}}}, func(args map[string]any) (string, error) {
		store, msg, err := openStoreOrMessage(fatbabyRoot, "secwatch")
		if msg != "" || err != nil {
			return msg, err
		}
		defer store.Close()
		latest, _ := store.LatestSequence(context.Background())
		recs, _ := store.ReadFrom(context.Background(), 1, int(latest))
		by := map[string]int{}
		total := 0
		for _, r := range recs {
			if r.Event.Type != "source_document_persisted" {
				continue
			}
			total++
			var doc intelligence.SourceDocument
			if json.Unmarshal(r.Event.Data, &doc) == nil {
				by[doc.Ticker]++
			}
		}
		b, _ := json.MarshalIndent(map[string]any{"total_source_documents": total, "by_ticker": by, "newssite_url": "http://localhost:8082"}, "", "  ")
		return string(b), nil
	})

	d.Register(ToolDef{Name: "fatbaby_newssite_status", Description: "Check whether the news site is reachable and return its current document count.", Parameters: ToolParameters{Type: "object", Properties: map[string]ToolPropSchema{}}}, func(args map[string]any) (string, error) {
		out, _ := runCommandWithTimeout([]string{"pgrep", "-af", "cmd/newssite"}, fatbabyRoot)
		running := parseProcOutput(out) != ""
		resp := map[string]any{"process_running": running, "url": "http://localhost:8082"}
		c := &http.Client{Timeout: 5 * time.Second}
		r, err := c.Get("http://localhost:8082/")
		if err != nil {
			resp["http_reachable"] = false
			resp["error"] = err.Error()
		} else {
			defer r.Body.Close()
			body, _ := io.ReadAll(r.Body)
			resp["http_reachable"] = r.StatusCode == 200
			resp["article_count_approx"] = strings.Count(string(body), "Read full document")
		}
		b, _ := json.MarshalIndent(resp, "", "  ")
		return string(b), nil
	})

	d.Register(ToolDef{Name: "fatbaby_process_status", Description: "Check if fatbaby pipeline processes are running. Covers all processes: secwatch, prwatch, prwatch-body, processor, entity-graph, eps-processor, eps-reconciler, schd13-watcher, form4-watcher, observation-watcher, dashboard, newssite, signalapi, feedserver, jon-agent.", Parameters: ToolParameters{Type: "object", Properties: map[string]ToolPropSchema{}}}, func(args map[string]any) (string, error) {
		names := []string{
			"cmd/secwatch", "cmd/prwatch", "cmd/prwatch-body", "cmd/processor",
			"cmd/entity-graph", "cmd/eps-processor", "cmd/eps-reconciler",
			"cmd/schd13-watcher", "cmd/form4-watcher", "cmd/observation-watcher",
			"cmd/dashboard", "cmd/newssite", "cmd/signalapi", "cmd/feedserver",
			"cmd/jon-agent",
			"cmd/form4-watcher",
			"cmd/dividend-watcher",
		}
		type ps struct {
			Name    string `json:"name"`
			Running bool   `json:"running"`
			PID     string `json:"pid,omitempty"`
		}
		var out []ps
		for _, n := range names {
			res, _ := runCommandWithTimeout([]string{"pgrep", "-af", n}, fatbabyRoot)
			p := ps{Name: n, Running: strings.TrimSpace(res) != ""}
			if p.Running {
				p.PID = strings.Fields(res)[0]
			}
			out = append(out, p)
		}
		dirs := map[string]bool{}
		for _, s := range []string{"secwatch", "prwatch", "prwatch-body", "entity-graph", "eps", "schd13"} {
			_, err := os.Stat(absStorePath(fatbabyRoot, s))
			dirs[s] = err == nil
		}
		b, _ := json.MarshalIndent(map[string]any{"processes": out, "store_dirs": dirs}, "", "  ")
		return string(b), nil
	})
	d.Register(ToolDef{Name: "fatbaby_check_env", Description: "Check processor env and prerequisites: Go binary, watchlist, ANTHROPIC_API_KEY, claude CLI path (needed by observation-watcher), and observation-watcher cursor state.", Parameters: ToolParameters{Type: "object", Properties: map[string]ToolPropSchema{}}}, func(args map[string]any) (string, error) {
		checks := []map[string]string{}

		// Go binary
		if _, err := exec.LookPath("go"); err == nil {
			checks = append(checks, map[string]string{"name": "go_path", "status": "ok", "value": "go found"})
		} else {
			checks = append(checks, map[string]string{"name": "go_path", "status": "missing", "detail": "go not in PATH"})
		}

		// Watchlist
		watch := filepath.Join(fatbabyRoot, "config", "watchlist.json")
		if b, err := os.ReadFile(watch); err != nil {
			checks = append(checks, map[string]string{"name": "watchlist", "status": "missing"})
		} else if json.Valid(b) {
			checks = append(checks, map[string]string{"name": "watchlist", "status": "ok", "value": "valid json"})
		} else {
			checks = append(checks, map[string]string{"name": "watchlist", "status": "invalid_json"})
		}

		// ANTHROPIC_API_KEY
		if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
			masked := key[:min(8, len(key))] + "..."
			checks = append(checks, map[string]string{"name": "anthropic_api_key", "status": "ok", "value": masked})
		} else {
			checks = append(checks, map[string]string{"name": "anthropic_api_key", "status": "missing", "detail": "ANTHROPIC_API_KEY not set — processor and emily-agent will not work"})
		}

		// claude CLI — try PATH first, then ~/.local/bin
		claudePath := ""
		if p, err := exec.LookPath("claude"); err == nil {
			claudePath = p
		} else {
			home, _ := os.UserHomeDir()
			for _, c := range []string{
				filepath.Join(home, ".local", "bin", "claude"),
				filepath.Join(home, "bin", "claude"),
				"/usr/local/bin/claude",
			} {
				if _, err := os.Stat(c); err == nil {
					claudePath = c
					break
				}
			}
		}
		if claudePath != "" {
			checks = append(checks, map[string]string{"name": "claude_cli", "status": "ok", "value": claudePath, "detail": "observation-watcher can invoke Claude Code"})
		} else {
			checks = append(checks, map[string]string{"name": "claude_cli", "status": "missing", "detail": "claude not found on PATH or in ~/.local/bin — autonomous observation-watcher loop will fail"})
		}

		// Observation-watcher cursor (indicates autonomous loop has run)
		cursorPath := filepath.Join(fatbabyRoot, "var", "emily-observations", ".last-processed")
		if b, err := os.ReadFile(cursorPath); err == nil {
			checks = append(checks, map[string]string{"name": "obs_watcher_cursor", "status": "ok", "value": strings.TrimSpace(string(b)), "detail": "autonomous loop has processed at least one observation"})
		} else {
			checks = append(checks, map[string]string{"name": "obs_watcher_cursor", "status": "no_runs", "detail": "observation-watcher has not yet processed any observations"})
		}

		// GitHub integration (optional — warn if not set)
		ghToken := os.Getenv("GITHUB_TOKEN")
		ghOwner := os.Getenv("GITHUB_OWNER")
		ghRepo := os.Getenv("GITHUB_REPO")
		if ghToken != "" && ghOwner != "" && ghRepo != "" {
			checks = append(checks, map[string]string{"name": "github_issues", "status": "ok", "value": ghOwner + "/" + ghRepo, "detail": "GitHub issue creation enabled for observations"})
		} else {
			checks = append(checks, map[string]string{"name": "github_issues", "status": "warn", "detail": "GITHUB_TOKEN/GITHUB_OWNER/GITHUB_REPO not set — observation GitHub issues disabled (optional)"})
		}

		rb, _ := json.MarshalIndent(map[string]any{"checks": checks}, "", "  ")
		return string(rb), nil
	})
	d.Register(ToolDef{Name: "fatbaby_count_signals", Description: "Count signal events in secwatch store.", Parameters: ToolParameters{Type: "object", Properties: map[string]ToolPropSchema{}}}, func(args map[string]any) (string, error) {
		store, msg, err := openStoreOrMessage(fatbabyRoot, "secwatch")
		if msg != "" || err != nil {
			return msg, err
		}
		defer store.Close()
		latest, _ := store.LatestSequence(context.Background())
		recs, _ := store.ReadFrom(context.Background(), 1, int(latest))
		generated, failed, discovered, sourcedocs := 0, 0, 0, 0
		for _, r := range recs {
			switch r.Event.Type {
			case "signal_generated":
				generated++
			case "signal_failed":
				failed++
			case "filing_discovered":
				discovered++
			case "source_document_persisted":
				sourcedocs++
			}
		}
		resp := map[string]any{"total_records": len(recs), "filing_discovered_count": discovered, "source_document_persisted_count": sourcedocs, "signal_generated_count": generated, "signal_failed_count": failed}
		b, _ := json.MarshalIndent(resp, "", "  ")
		return string(b), nil
	})

	d.Register(ToolDef{Name: "fatbaby_write_observation", Description: "Publish a structured observation to var/emily-observations/latest.json (and a timestamped archive copy). This is the handoff point for the Emily ↔ Claude Code feedback loop — Claude Code reads latest.json as its task prompt.", Parameters: ToolParameters{Type: "object", Properties: map[string]ToolPropSchema{
		"summary":  {Type: "string", Description: "One-line headline of what Emily observed."},
		"severity": {Type: "string", Description: "info|warn|error — how urgent this is."},
		"findings": {Type: "string", Description: "Detailed multi-line description of what was observed and which processes/tickers are involved."},
		"suggested_fix": {Type: "string", Description: "Concrete suggestion for what Claude Code should change in the source, if any."},
	}, Required: []string{"summary", "findings"}}}, func(args map[string]any) (string, error) {
		summary, _ := args["summary"].(string)
		findings, _ := args["findings"].(string)
		severity, _ := args["severity"].(string)
		suggested, _ := args["suggested_fix"].(string)
		if strings.TrimSpace(summary) == "" || strings.TrimSpace(findings) == "" {
			return "", errors.New("summary and findings are required")
		}
		if severity == "" {
			severity = "info"
		}
		dir := filepath.Join(fatbabyRoot, "var", "emily-observations")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
		now := time.Now().UTC()
		obs := map[string]any{
			"timestamp":     now.Format(time.RFC3339),
			"summary":       summary,
			"severity":      severity,
			"findings":      findings,
			"suggested_fix": suggested,
		}
		b, err := json.MarshalIndent(obs, "", "  ")
		if err != nil {
			return "", err
		}
		latest := filepath.Join(dir, "latest.json")
		if err := os.WriteFile(latest, b, 0o644); err != nil {
			return "", err
		}
		archive := filepath.Join(dir, now.Format("20060102T150405Z")+".json")
		if err := os.WriteFile(archive, b, 0o644); err != nil {
			return "", err
		}
		result := fmt.Sprintf("wrote observation severity=%s latest=%s archive=%s", severity, latest, archive)

		// Create a GitHub issue for audit trail and sprint planning.
		// Best-effort: runs in background so it never blocks the write.
		go createGitHubIssue(summary, findings, suggested, severity)

		// Auto-commit to Emily Prime's integration layer for high/critical findings.
		// Best-effort: failures are logged but don't block the observation write.
		if severity == "error" || severity == "warn" || severity == "critical" || severity == "high" {
			primeObsDir := os.Getenv("EMILY_INTEGRATION_DIR")
			if primeObsDir == "" {
				primeObsDir = filepath.Join(fatbabyRoot, "..", "EMILY", "signals", "observations")
			} else {
				primeObsDir = filepath.Join(primeObsDir, "observations")
			}
			if mkErr := os.MkdirAll(primeObsDir, 0o755); mkErr == nil {
				enriched := map[string]any{
					"timestamp":        now.Format(time.RFC3339),
					"source":           "fatbaby-emily",
					"observation_type": "anomaly",
					"severity":         severity,
					"summary":          summary,
					"findings":         findings,
					"suggested_fix":    suggested,
				}
				slug := slugifySimple(summary, 32)
				fname := strings.ReplaceAll(now.Format(time.RFC3339), ":", "") + "-" + slug + ".json"
				outPath := filepath.Join(primeObsDir, fname)
				if pb, jErr := json.MarshalIndent(enriched, "", "  "); jErr == nil {
					if wErr := os.WriteFile(outPath, pb, 0o644); wErr == nil {
						primeRoot := filepath.Dir(filepath.Dir(primeObsDir))
						rel, _ := filepath.Rel(primeRoot, outPath)
						exec.Command("git", "-C", primeRoot, "add", rel).Run()
						exec.Command("git", "-C", primeRoot, "commit", "-m", "observation from fatbaby: "+summary,
							"--author=FatBaby-Emily <fatbaby-emily@agent.local>").Run()
						result += " · committed to emily-prime"
					}
				}
			}
		}
		return result, nil
	})

	d.Register(ToolDef{Name: "fatbaby_read_observation", Description: "Read back the most recent observation Emily published. Useful before writing a new one to avoid duplicating a still-open finding.", Parameters: ToolParameters{Type: "object", Properties: map[string]ToolPropSchema{}}}, func(args map[string]any) (string, error) {
		p := filepath.Join(fatbabyRoot, "var", "emily-observations", "latest.json")
		b, err := os.ReadFile(p)
		if err != nil {
			if os.IsNotExist(err) {
				return "no observations yet", nil
			}
			return "", err
		}
		return string(b), nil
	})

	// fatbaby_publish_commentary publishes an Emily-authored governance article to
	// var/commentary/articles.ndjson — picked up by the newssite for rendering.
	d.Register(ToolDef{
		Name:        "fatbaby_publish_commentary",
		Description: "Publish an Emily-authored governance article to the newssite. Articles appear on the ticker page alongside SEC filings. Use for signal commentary, cross-ticker summaries, governance alerts, or EPS reconciliation narratives.",
		Parameters: ToolParameters{
			Type: "object",
			Properties: map[string]ToolPropSchema{
				"ticker":      {Type: "string", Description: "Ticker symbol the article is about (e.g. SCHW). Leave empty for cross-ticker articles."},
				"headline":    {Type: "string", Description: "Article headline (one sentence)."},
				"body":        {Type: "string", Description: "Full article text in plain prose. No markdown."},
				"kind":        {Type: "string", Description: "Article type: signal_commentary|governance_alert|eps_reconciliation|cross_ticker_summary"},
				"filing_date": {Type: "string", Description: "YYYY-MM-DD of the SEC filing being discussed (if applicable)."},
				"signal_ids":  {Type: "string", Description: "Comma-separated signal IDs this article references (optional)."},
			},
			Required: []string{"headline", "body"},
		},
	}, func(args map[string]any) (string, error) {
		headline, _ := args["headline"].(string)
		body, _ := args["body"].(string)
		if strings.TrimSpace(headline) == "" || strings.TrimSpace(body) == "" {
			return "", errors.New("headline and body are required")
		}
		ticker, _ := args["ticker"].(string)
		kind, _ := args["kind"].(string)
		filingDate, _ := args["filing_date"].(string)
		sigIDsRaw, _ := args["signal_ids"].(string)
		var sigIDs []string
		for _, s := range strings.Split(sigIDsRaw, ",") {
			if s = strings.TrimSpace(s); s != "" {
				sigIDs = append(sigIDs, s)
			}
		}
		if kind == "" {
			kind = "signal_commentary"
		}
		now := time.Now().UTC()
		id := "commentary-" + strings.ToLower(strings.TrimSpace(ticker)) + "-" + now.Format("20060102T150405Z")
		preview := []rune(body)
		if len(preview) > 300 {
			preview = preview[:300]
		}
		article := map[string]any{
			"id":          id,
			"ticker":      strings.ToUpper(strings.TrimSpace(ticker)),
			"headline":    headline,
			"body":        body,
			"preview":     strings.TrimSpace(string(preview)),
			"byline":      "Emily — Signal Intelligence",
			"kind":        kind,
			"filing_date": filingDate,
			"published_at": now.Format(time.RFC3339),
			"signal_ids":  sigIDs,
		}
		dir := filepath.Join(fatbabyRoot, "var", "commentary")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
		b, err := json.Marshal(article)
		if err != nil {
			return "", err
		}
		f, err := os.OpenFile(filepath.Join(dir, "articles.ndjson"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return "", err
		}
		defer f.Close()
		if _, err := f.Write(append(b, '\n')); err != nil {
			return "", err
		}
		return fmt.Sprintf("published commentary id=%s ticker=%s to %s", id, ticker, dir), nil
	})

	// fatbaby_commit_to_prime publishes the current observation to Emily Prime's
	// integration layer at EMILY_INTEGRATION_DIR (signals/observations/).
	// This is the handoff that closes the Emily Prime ↔ FatBaby loop.
	d.Register(ToolDef{
		Name:        "fatbaby_commit_to_prime",
		Description: "Publish the current FatBaby observation to Emily Prime's integration layer (signals/observations/). Emily Prime will pick it up, triage it, and may issue a directed task back. Call this after fatbaby_write_observation when the finding is strategically significant.",
		Parameters: ToolParameters{
			Type: "object",
			Properties: map[string]ToolPropSchema{
				"observation_type":      {Type: "string", Description: "anomaly|improvement|escalation|status"},
				"requires_ceo_visibility": {Type: "string", Description: "true|false — whether this should go to the CEO"},
			},
		},
	}, func(args map[string]any) (string, error) {
		// Read the current latest.json observation.
		latest := filepath.Join(fatbabyRoot, "var", "emily-observations", "latest.json")
		b, err := os.ReadFile(latest)
		if err != nil {
			if os.IsNotExist(err) {
				return "", errors.New("no observation to commit — call fatbaby_write_observation first")
			}
			return "", err
		}

		primeDir := os.Getenv("EMILY_INTEGRATION_DIR")
		if primeDir == "" {
			// Try sibling directory heuristic: ../EMILY/signals/observations
			primeDir = filepath.Join(fatbabyRoot, "..", "EMILY", "signals", "observations")
		} else {
			primeDir = filepath.Join(primeDir, "observations")
		}
		if err := os.MkdirAll(primeDir, 0o755); err != nil {
			return "", fmt.Errorf("prime integration dir: %w", err)
		}

		// Enrich the observation with integration-layer fields.
		var obs map[string]any
		if err := json.Unmarshal(b, &obs); err != nil {
			return "", fmt.Errorf("parse observation: %w", err)
		}
		obs["source"] = "fatbaby-emily"
		if ot := args["observation_type"]; ot != nil {
			obs["observation_type"] = ot
		}
		if cv := args["requires_ceo_visibility"]; cv == "true" {
			obs["requires_ceo_visibility"] = true
		}

		ts, _ := obs["timestamp"].(string)
		if ts == "" {
			ts = time.Now().UTC().Format(time.RFC3339)
		}
		summary, _ := obs["summary"].(string)
		slug := slugifySimple(summary, 32)
		fname := strings.ReplaceAll(ts, ":", "") + "-" + slug + ".json"
		outPath := filepath.Join(primeDir, fname)

		enriched, err := json.MarshalIndent(obs, "", "  ")
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(outPath, enriched, 0o644); err != nil {
			return "", fmt.Errorf("write to prime: %w", err)
		}

		// Git commit if inside a repo.
		primeRoot := filepath.Dir(filepath.Dir(primeDir))
		rel, _ := filepath.Rel(primeRoot, outPath)
		exec.Command("git", "-C", primeRoot, "add", rel).Run()
		exec.Command("git", "-C", primeRoot, "commit", "-m", "observation from fatbaby: "+summary,
			"--author=FatBaby-Emily <fatbaby-emily@agent.local>").Run()

		return fmt.Sprintf("committed to prime: %s", fname), nil
	})

	// fatbaby_check_prime_tasks reads pending directed tasks from Emily Prime's
	// signals/tasks/ directory. This is the mirror of fatbaby_commit_to_prime:
	// Prime writes tasks here, FatBaby reads and acts on them.
	d.Register(ToolDef{
		Name:        "fatbaby_check_prime_tasks",
		Description: "Check for directed tasks issued by Emily Prime. Reads EMILY/signals/tasks/ and returns any task files that haven't been processed yet. Call this when asked to check for tasks or instructions from Emily Prime.",
		Parameters:  ToolParameters{Type: "object", Properties: map[string]ToolPropSchema{}},
	}, func(args map[string]any) (string, error) {
		tasksDir := os.Getenv("EMILY_PRIME_TASKS_DIR")
		if tasksDir == "" {
			tasksDir = filepath.Join(fatbabyRoot, "..", "EMILY", "signals", "tasks")
		}
		entries, err := os.ReadDir(tasksDir)
		if err != nil {
			if os.IsNotExist(err) {
				return "Emily Prime tasks directory not found at " + tasksDir, nil
			}
			return "", fmt.Errorf("read tasks dir: %w", err)
		}
		// Read cursor to identify already-processed tasks.
		cursorPath := filepath.Join(tasksDir, ".last-processed")
		lastProcessed := ""
		if b, err := os.ReadFile(cursorPath); err == nil {
			lastProcessed = strings.TrimSpace(string(b))
		}
		var pending []map[string]any
		var allTasks []map[string]any
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(tasksDir, e.Name()))
			if err != nil {
				continue
			}
			var task map[string]any
			if err := json.Unmarshal(data, &task); err != nil {
				continue
			}
			task["_filename"] = e.Name()
			allTasks = append(allTasks, task)
			if e.Name() > lastProcessed {
				pending = append(pending, task)
			}
		}
		if len(allTasks) == 0 {
			return "no tasks found in " + tasksDir, nil
		}
		result := map[string]any{
			"tasks_dir":      tasksDir,
			"total_tasks":    len(allTasks),
			"pending_tasks":  len(pending),
			"last_processed": lastProcessed,
			"pending":        pending,
		}
		if len(pending) == 0 {
			result["message"] = "all tasks already processed by observation-watcher"
		}
		b, _ := json.MarshalIndent(result, "", "  ")
		return string(b), nil
	})

	// ── one-shot runners for governance pipeline ────────────────────────────

	d.Register(ToolDef{Name: "fatbaby_run_entity_graph_once", Description: "Run one batch of the entity-graph governance processor and wait for completion (~30-90s depending on filing backlog). Reads 8-K filings from var/secwatch, updates var/entity-graph (nodes/edges/signals), and publishes an observation to var/emily-observations. Call fatbaby_read_observation afterward to see what was found.", Parameters: ToolParameters{Type: "object", Properties: map[string]ToolPropSchema{}}}, func(args map[string]any) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "go", "run", "./cmd/entity-graph",
			"-store", "./var/secwatch",
			"-graph-dir", "./var/entity-graph",
			"-obs-dir", "./var/emily-observations",
			"-rules", "./config/entity-graph-rules.json",
			"-one-shot")
		cmd.Dir = fatbabyRoot
		out, err := cmd.CombinedOutput()
		s := strings.TrimSpace(string(out))
		if len(s) > 8000 {
			s = s[:8000]
		}
		return s, err
	})

	d.Register(ToolDef{Name: "fatbaby_run_schd13_once", Description: "Run one pass of the Schedule 13D/13G watcher and wait for completion. Queries EDGAR for activist filings on all watchlisted CIKs, writes var/schd13/filings.ndjson, and updates activist_risk signal accuracy records. Run after entity-graph has produced activist_risk signals.", Parameters: ToolParameters{Type: "object", Properties: map[string]ToolPropSchema{}}}, func(args map[string]any) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "go", "run", "./cmd/schd13-watcher",
			"-watchlist", "./config/watchlist.json",
			"-graph-dir", "./var/entity-graph",
			"-out-dir", "./var/schd13",
			"-one-shot")
		cmd.Dir = fatbabyRoot
		out, err := cmd.CombinedOutput()
		s := strings.TrimSpace(string(out))
		if len(s) > 8000 {
			s = s[:8000]
		}
		return s, err
	})

	d.Register(ToolDef{Name: "fatbaby_run_eps_reconcile_once", Description: "Run one pass of the EPS reconciler and wait for completion. Scans var/secwatch for 8-K earnings releases, matches pending oracle cases in var/eps/oracle.ndjson by ticker+quarter, and writes confirmed/contradicts verdicts. Returns oracle precision summary.", Parameters: ToolParameters{Type: "object", Properties: map[string]ToolPropSchema{}}}, func(args map[string]any) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "go", "run", "./cmd/eps-reconciler",
			"-store", "./var/secwatch",
			"-eps-dir", "./var/eps",
			"-one-shot")
		cmd.Dir = fatbabyRoot
		out, err := cmd.CombinedOutput()
		s := strings.TrimSpace(string(out))
		if len(s) > 4000 {
			s = s[:4000]
		}
		return s, err
	})

	// ── EPS feed status ─────────────────────────────────────────────────────

	d.Register(ToolDef{Name: "fatbaby_eps_status", Description: "Show the current state of the EPS headlines feed: oracle case counts (pending/confirmed/contradicts), extraction precision, and number of published articles. Use to assess how well the EPS extractor is performing and whether the reconciler has run.", Parameters: ToolParameters{Type: "object", Properties: map[string]ToolPropSchema{}}}, func(args map[string]any) (string, error) {
		epsDir := filepath.Join(fatbabyRoot, "var", "eps")

		// Count oracle cases.
		oracleTotal, oraclePending, oracleConfirmed, oracleContradicts := 0, 0, 0, 0
		if f, err := os.Open(filepath.Join(epsDir, "oracle.ndjson")); err == nil {
			sc := bufio.NewScanner(f)
			sc.Buffer(make([]byte, 1<<20), 1<<20)
			for sc.Scan() {
				var c struct {
					Verdict string `json:"verdict"`
				}
				if json.Unmarshal(sc.Bytes(), &c) == nil {
					oracleTotal++
					switch c.Verdict {
					case "confirmed":
						oracleConfirmed++
					case "contradicts":
						oracleContradicts++
					case "pending":
						oraclePending++
					}
				}
			}
			f.Close()
		}

		precision := 0.0
		resolved := oracleConfirmed + oracleContradicts
		if resolved > 0 {
			precision = float64(oracleConfirmed) / float64(resolved)
		}

		// Count articles.
		articleCount := 0
		if f, err := os.Open(filepath.Join(epsDir, "articles.ndjson")); err == nil {
			sc := bufio.NewScanner(f)
			sc.Buffer(make([]byte, 1<<20), 1<<20)
			for sc.Scan() {
				if len(strings.TrimSpace(sc.Text())) > 0 {
					articleCount++
				}
			}
			f.Close()
		}

		b, _ := json.MarshalIndent(map[string]any{
			"oracle_total":       oracleTotal,
			"oracle_pending":     oraclePending,
			"oracle_confirmed":   oracleConfirmed,
			"oracle_contradicts": oracleContradicts,
			"precision":          precision,
			"articles_published": articleCount,
			"oracle_path":        "var/eps/oracle.ndjson",
			"articles_path":      "var/eps/articles.ndjson",
		}, "", "  ")
		return string(b), nil
	})

	// ── Press release counts ────────────────────────────────────────────────

	d.Register(ToolDef{
		Name:        "fatbaby_read_governance_signals",
		Description: "Read governance signals from var/entity-graph/signals.ndjson. Returns a summary by ticker (signal count, severity breakdown, latest filing date) plus the most recent high/critical signals. Use to understand the current governance intelligence picture without running a full entity-graph batch.",
		Parameters: ToolParameters{Type: "object", Properties: map[string]ToolPropSchema{
			"ticker": {Type: "string", Description: "filter to a specific ticker symbol (optional, case-insensitive)"},
			"limit":  {Type: "number", Description: "max signals to return in detail list (default 20)"},
		}},
	}, func(args map[string]any) (string, error) {
		sigPath := filepath.Join(fatbabyRoot, "var", "entity-graph", "signals.ndjson")
		f, err := os.Open(sigPath)
		if err != nil {
			if os.IsNotExist(err) {
				return `{"error":"signals.ndjson not found — run fatbaby_run_entity_graph_once first"}`, nil
			}
			return "", fmt.Errorf("open signals: %w", err)
		}
		defer f.Close()

		filterTicker := strings.ToUpper(strings.TrimSpace(fmt.Sprintf("%v", args["ticker"])))
		if filterTicker == "<NIL>" || filterTicker == "" {
			filterTicker = ""
		}
		limit := 20
		if v, ok := args["limit"].(float64); ok && v > 0 {
			limit = int(v)
		}

		type sigRecord struct {
			Signal struct {
				SignalID       string `json:"signal_id"`
				Type           string `json:"type"`
				Ticker         string `json:"ticker"`
				Entity         string `json:"entity"`
				Severity       string `json:"severity"`
				Score          float64 `json:"score"`
				FilingDate     string `json:"filing_date"`
				Interpretation string `json:"interpretation"`
			} `json:"signal"`
		}

		type tickerSummary struct {
			Total    int            `json:"total"`
			Severity map[string]int `json:"by_severity"`
			Latest   string         `json:"latest_filing_date"`
		}

		tickerSums := map[string]*tickerSummary{}
		var recent []map[string]string

		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			// Try wrapped {"signal":{...}} format first, then flat.
			var rec sigRecord
			if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
				continue
			}
			s := rec.Signal
			if s.SignalID == "" {
				// Try flat signal record.
				type flatSig struct {
					SignalID       string  `json:"signal_id"`
					Type           string  `json:"type"`
					Ticker         string  `json:"ticker"`
					Entity         string  `json:"entity"`
					Severity       string  `json:"severity"`
					Score          float64 `json:"score"`
					FilingDate     string  `json:"filing_date"`
					Interpretation string  `json:"interpretation"`
				}
				var fs flatSig
				if err2 := json.Unmarshal(sc.Bytes(), &fs); err2 != nil {
					continue
				}
				s.SignalID = fs.SignalID
				s.Type = fs.Type
				s.Ticker = fs.Ticker
				s.Entity = fs.Entity
				s.Severity = fs.Severity
				s.Score = fs.Score
				s.FilingDate = fs.FilingDate
				s.Interpretation = fs.Interpretation
			}
			if s.SignalID == "" {
				continue
			}
			t := strings.ToUpper(s.Ticker)
			if filterTicker != "" && t != filterTicker {
				continue
			}
			if _, ok := tickerSums[t]; !ok {
				tickerSums[t] = &tickerSummary{Severity: map[string]int{}}
			}
			ts := tickerSums[t]
			ts.Total++
			ts.Severity[s.Severity]++
			if s.FilingDate > ts.Latest {
				ts.Latest = s.FilingDate
			}
			if (s.Severity == "high" || s.Severity == "critical") && len(recent) < limit {
				recent = append(recent, map[string]string{
					"signal_id":      s.SignalID,
					"type":           s.Type,
					"ticker":         t,
					"entity":         s.Entity,
					"severity":       s.Severity,
					"score":          fmt.Sprintf("%.3f", s.Score),
					"filing_date":    s.FilingDate,
					"interpretation": s.Interpretation,
				})
			}
		}
		if err := sc.Err(); err != nil {
			return "", fmt.Errorf("scan signals: %w", err)
		}

		b, _ := json.MarshalIndent(map[string]any{
			"ticker_summaries":    tickerSums,
			"high_critical_signals": recent,
			"total_tickers":       len(tickerSums),
		}, "", "  ")
		return string(b), nil
	})

	d.Register(ToolDef{Name: "fatbaby_count_press_releases", Description: "Count press releases in the prwatch pipeline: how many have been discovered (pr_discovered) and how many have had their full body fetched (pr_body_fetched). Use to verify prwatch and prwatch-body are making progress.", Parameters: ToolParameters{Type: "object", Properties: map[string]ToolPropSchema{}}}, func(args map[string]any) (string, error) {
		discovered, fetched, failed := 0, 0, 0

		discStore, _, err := openStoreOrMessage(fatbabyRoot, "prwatch")
		if err == nil {
			defer discStore.Close()
			latest, _ := discStore.LatestSequence(context.Background())
			recs, _ := discStore.ReadFrom(context.Background(), 1, int(latest))
			for _, r := range recs {
				if r.Event.Type == "pr_discovered" {
					discovered++
				}
			}
		}

		bodyStore, _, err := openStoreOrMessage(fatbabyRoot, "prwatch-body")
		if err == nil {
			defer bodyStore.Close()
			latest, _ := bodyStore.LatestSequence(context.Background())
			recs, _ := bodyStore.ReadFrom(context.Background(), 1, int(latest))
			for _, r := range recs {
				switch r.Event.Type {
				case "pr_body_fetched":
					fetched++
				case "pr_body_failed":
					failed++
				}
			}
		}

		b, _ := json.MarshalIndent(map[string]any{
			"pr_discovered":   discovered,
			"pr_body_fetched": fetched,
			"pr_body_failed":  failed,
		}, "", "  ")
		return string(b), nil
	})
}

const emilySystemPrompt = `You are Emily, the operations agent and signal analyst for prrject-fatbaby — a Go-based financial signal intelligence pipeline.

You have three roles:

1. **Ops agent** — start/stop all pipeline processes, read logs, check health, count documents.
2. **Governance analyst** — read and interpret entity-graph governance signals, director risk, board relationships, and what the data means for specific companies or people.
3. **EPS analyst** — monitor the EPS headlines feed, its oracle precision, and identify extraction failures for the self-improvement loop.

When a user asks about signals, directors, companies, governance risk, or EPS data: use your tools first. Give a clear, opinionated interpretation — don't dump raw JSON. Synthesise: what does this mean, how serious is it, what should a portfolio manager do?

## Signal type glossary

| Signal | What it means |
|--------|--------------|
| director_friction | Director approval below 85% — activist targeting, board misalignment, or ESG pushback. Watch for declining trend. |
| nomination_rejection | Approval below 50% — under majority-voting standards the director must submit resignation. Critical if board refuses to accept. |
| director_decay | Approval declining year-over-year — director is on borrowed time, likely not re-nominated in 12-18 months. |
| high_trust_director | Approval above 95% — stable, low-controversy board seat. Can mask rubber-stamp dynamics. |
| governance_entrenchment | Proposal blocked by supermajority-of-outstanding threshold despite clear vote intent — structural defense against shareholder preference. Strong M&A defense indicator. |
| activist_risk | Composite: entrenchment + friction co-occurring within 12 months. Base rate: activist 13D filed within 6 months in ~60% of similar cases. |
| director_link | Friction director sits on multiple boards — governance risk propagates across their portfolio. |
| family_control | Director name matches founder/family keyword — may represent concentrated founder control. |
| broker_nonvote_anomaly | Broker non-votes above 12% — elevated retail/street-name voting. |
| compensation_concern | Say-on-pay opposition above 30% — ESG funds or proxy advisors agitating on executive pay. |
| abstention_spike | Abstention rate above 10% — shareholder confusion, inadequate disclosure, or coordinated protest. |
| auditor_change | Company switched public accounting firm — audit quality dispute, regulatory pressure, or pre-transaction restructuring. |
| governance_health_index | Composite [0,1] score. Below 0.5 = high governance risk. Score of ~0.30 (e.g. SCHW 2026) = critical. Above 0.80 = clean board. |

## Full pipeline architecture

  SEC EDGAR feed:
    secwatch      → polls EDGAR → filing_discovered                   → var/secwatch
    processor     → fetches/cleans 8-Ks → source_document_persisted   → var/secwatch
    entity-graph  → reads 8-K source docs → extracts vote signals
                    → writes nodes/edges/signals.ndjson               → var/entity-graph
                    → publishes governance observation                 → var/emily-observations
    schd13-watcher → polls EDGAR for SC 13D/13G filings
                    → writes filings.ndjson                           → var/schd13
                    → updates activist_risk accuracy records          → var/entity-graph/accuracy.ndjson

  Press release / EPS feed:
    prwatch       → polls PR Newswire → pr_discovered                 → var/prwatch
    prwatch-body  → fetches full body → pr_body_fetched               → var/prwatch-body
    eps-processor → reads press releases → extracts EPS headlines
                    → writes articles.ndjson + oracle cases           → var/eps
    eps-reconciler → reads 8-K source docs → reconciles EPS extractions
                    → writes confirmed/contradicts verdicts           → var/eps/oracle.ndjson

  Feedback loop:
    observation-watcher → polls var/emily-observations/latest.json
                        → invokes Claude Code when entity-graph publishes a finding
                        → Claude edits config/entity-graph-rules.json or parser.go

  UI / downstream:
    newssite      → reads source_document_persisted → HTML reader     → :8082
    dashboard     → SSE dashboard                                     → :8080
    signalapi     → HTTP API for querying signals
    feedserver    → TCP framed feed for downstream consumers

## Startup sequences

### Get the news site showing SEC filings:
1. fatbaby_check_env
2. fatbaby_run_secwatch_once           (seeds filing_discovered events, ~60s)
3. fatbaby_start_process processor
4. fatbaby_start_process newssite
5. fatbaby_count_source_documents      (poll until count > 0)

### Inspect governance signals (fast, no pipeline run needed):
1. fatbaby_read_governance_signals           (read current signals from var/entity-graph/signals.ndjson)
2. fatbaby_read_governance_signals ticker=SCHW  (filter to one company)

### Run a governance observation batch:
1. fatbaby_run_entity_graph_once       (processes 8-K votes → signals → observation, ~30-90s)
2. fatbaby_read_observation            (read what entity-graph found and published)
3. fatbaby_read_governance_signals     (inspect updated signal set)
4. fatbaby_run_schd13_once             (optionally: check activist 13D filings + accuracy)

### Start governance pipeline as background daemons:
1. fatbaby_start_process entity-graph
2. fatbaby_start_process schd13-watcher
3. fatbaby_start_process observation-watcher
4. fatbaby_read_log entity-graph       (confirm startup after 15s)

### Start EPS feed:
1. fatbaby_start_process prwatch
2. fatbaby_start_process prwatch-body
3. fatbaby_start_process eps-processor
4. fatbaby_start_process eps-reconciler   (or run one-shot: fatbaby_run_eps_reconcile_once)
5. fatbaby_eps_status                     (check oracle precision)

## Reading governance observations

The entity-graph process publishes to var/emily-observations/latest.json after each batch.
Call fatbaby_read_observation to read it. Entity-graph observations have these key fields:

  source:           "entity-graph" (vs Emily's own generic observations)
  status:           "ok" | "needs_attention"
  subject:          ticker being reported on
  signals_by_type:  map of signal type → count for this batch
  gaps:             list of parse failures or missing signals
  parse_errors:     details on any 8-K Item 5.07 parse failures
  accuracy_scores:  activist_risk prediction precision (confirmed/total)
  request_for_claude: what Claude Code should do if status=needs_attention

When status=needs_attention:
- Check gaps[] for parse failures — tell the user which tickers had issues
- Check accuracy_scores[] for declining precision — if activist_risk precision < 0.5, worth noting
- If parse_errors are present, call fatbaby_write_observation to escalate to Claude Code

When status=ok:
- Report the signal counts, confirm the governance health index for each ticker
- If accuracy_scores show confirmed predictions, highlight this (our predictions are working)

## Operating rules

- Always use tools to check actual state before making claims.
- Do not guess whether a process is running — call fatbaby_process_status.
- When starting processes, check logs with fatbaby_read_log after 10–15 seconds.
- Prefer one-shot runners (fatbaby_run_entity_graph_once, fatbaby_run_secwatch_once) over background daemons for diagnostic runs.
- The processor uses a stub LLM; signal_generated events are stubs. source_document_persisted events contain real cleaned filing text.
- EDGAR rate limit: 10 req/s; secwatch defaults to 2 RPS — do not advise raising beyond 5.
- var/secwatch = SEC feed. var/prwatch + var/prwatch-body = press release feed. var/entity-graph = governance signals. var/eps = EPS oracle + articles.

## EPS analyst rules

- Call fatbaby_eps_status to see oracle precision before making claims about EPS accuracy.
- Precision = confirmed / (confirmed + contradicts). Below 0.80 suggests extractor issues worth reporting.
- If eps-processor has run but oracle_total is 0, press releases haven't contained extractable EPS — check fatbaby_count_press_releases to verify body events are flowing.
- Contradicted cases are high-value training examples. If any exist, describe what likely went wrong (prior-period contamination, GAAP/non-GAAP mislabel, sign error).
- The EPS self-improvement loop: emily observes low precision → write observation → Claude Code refines internal/eps/extract.go → precision improves next cycle.

## Signal analyst rules

- When asked about signals, companies, or governance risk: call fatbaby_signal_summary or fatbaby_query_signals first.
- Synthesise into a plain-English assessment: what it means, how serious, likely cause, recommended action.
- Use fatbaby_entity_graph for director backgrounds and approval trends.
- If entity-graph store is empty, explain: run fatbaby_run_entity_graph_once to process 8-K filings.
- Confidence scores: 0.8+ = high, 0.6–0.8 = moderate, below 0.6 = speculative.
- The activist_risk signal is composite — always explain both components (entrenchment + friction).
- governance_health_index below 0.5 = high risk; above 0.80 = clean. This is the single rankable number for cross-company comparison.

## Reporting findings to Claude Code

When you detect a problem that needs source code changes — parse errors, stalled processors, low EPS precision, dropped tickers, suspicious zero-counts — call fatbaby_write_observation. Claude Code reads var/emily-observations/latest.json as its task prompt.

Before writing, call fatbaby_read_observation to avoid duplicating an open finding. Do not write observations for transient state; only for issues that need code changes.

## Emily Prime integration

Emily Prime can issue directed tasks to you via the integration layer. When asked to check for tasks or instructions from Emily Prime, call fatbaby_check_prime_tasks — it reads EMILY/signals/tasks/ and returns any pending tasks with their description, type, priority, and context. Act on pending tasks the same way you act on user requests. If a task is complex enough to require source code changes, use fatbaby_write_observation so Claude Code picks it up.`

var chatHTML = `<!doctype html><html><head><meta charset="utf-8"><title>Emily — fatbaby ops</title><style>body{font-family:system-ui,sans-serif;max-width:900px;margin:20px auto}#history{height:65vh;overflow:auto;border:1px solid #ccc;padding:12px}textarea{width:100%;height:90px}button{margin-top:8px}</style></head><body><h2>Emily — fatbaby ops</h2><div id="history"></div><p id="thinking" style="display:none">thinking…</p><textarea id="input" placeholder="Type message; Ctrl+Enter to send"></textarea><br><button id="send">Send</button><script>const history=[];const div=document.getElementById('history');const thinking=document.getElementById('thinking');function render(){div.innerHTML='';for(const m of history){const d=document.createElement('div');d.innerHTML='<b>'+m.role+':</b> '+(m.content||'');div.appendChild(d);if(m.role==='assistant'&&m.tool_calls){for(const t of m.tool_calls){const det=document.createElement('details');det.innerHTML='<summary>'+t.tool+'</summary><pre>'+(t.result||'')+'</pre>';div.appendChild(det)}}}div.scrollTop=div.scrollHeight}async function send(){const v=document.getElementById('input').value.trim();if(!v)return;history.push({role:'user',content:v});document.getElementById('input').value='';render();thinking.style.display='block';const r=await fetch('/chat',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({messages:history.map(x=>({role:x.role,content:x.content}))})});const j=await r.json();thinking.style.display='none';history.push({role:'assistant',content:j.reply,tool_calls:j.tool_calls||[]});render()}document.getElementById('send').onclick=send;document.getElementById('input').addEventListener('keydown',e=>{if(e.ctrlKey&&e.key==='Enter')send()});</script></body></html>`

type Server struct {
	cfg         Config
	d           *ToolDispatcher
	limiter     *rateLimiter
	client      *http.Client
	mu          sync.Mutex
	anthropicURL string
	// IDUNA agent token (spec HQ-SPEC-IAM-095 §3.1)
	idunaMu      sync.RWMutex
	idunaToken   string    // current JWT; empty if IDUNA not configured
	idunaTokenExp time.Time // expiry of idunaToken
}

// idunaAgentCfg holds the configuration for Emily's IDUNA agent identity.
type idunaAgentCfg struct {
	BaseURL     string // e.g. "https://iam.farthq.internal"
	AgentName   string // e.g. "EMILY"
	AgentSecret string // raw credential (never logged)
}

func loadIDUNAAgentCfg() (idunaAgentCfg, bool) {
	cfg := idunaAgentCfg{
		BaseURL:     os.Getenv("IDUNA_BASE_URL"),
		AgentName:   os.Getenv("IDUNA_AGENT_NAME"),
		AgentSecret: os.Getenv("IDUNA_AGENT_SECRET"),
	}
	return cfg, cfg.BaseURL != "" && cfg.AgentName != "" && cfg.AgentSecret != ""
}

// acquireIDUNAToken calls IDUNA's /api/v1/auth/agent endpoint and stores the
// returned JWT. Safe to call concurrently; uses idunaToken write lock.
func (s *Server) acquireIDUNAToken(cfg idunaAgentCfg) error {
	body, _ := json.Marshal(map[string]string{
		"agent_name":   cfg.AgentName,
		"agent_secret": cfg.AgentSecret,
	})
	req, err := http.NewRequest("POST", cfg.BaseURL+"/api/v1/auth/agent", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("call IDUNA: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("IDUNA returned status %d", resp.StatusCode)
	}
	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("parse IDUNA response: %w", err)
	}
	s.idunaMu.Lock()
	s.idunaToken = result.AccessToken
	if result.ExpiresIn > 0 {
		s.idunaTokenExp = time.Now().UTC().Add(time.Duration(result.ExpiresIn) * time.Second)
	} else {
		s.idunaTokenExp = time.Now().UTC().Add(time.Hour)
	}
	s.idunaMu.Unlock()
	return nil
}

// idunaTokenFresh returns true when the token is present and not within 5min of expiry.
func (s *Server) idunaTokenFresh() bool {
	s.idunaMu.RLock()
	defer s.idunaMu.RUnlock()
	return s.idunaToken != "" && time.Now().UTC().Before(s.idunaTokenExp.Add(-5*time.Minute))
}

const defaultAnthropicURL = "https://api.anthropic.com/v1/messages"

func NewServer(cfg Config) *Server {
	d := NewToolDispatcher()
	registerGitTools(d, cfg.ConversationDir)
	registerFatbabyTools(d, cfg.FatbabyRoot)
	registerSignalIntelligenceTools(d, cfg.FatbabyRoot)
	return &Server{cfg: cfg, d: d, limiter: newRateLimiter(cfg.RateLimitRPM), client: &http.Client{Timeout: 90 * time.Second}, anthropicURL: defaultAnthropicURL}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			http.Error(w, "internal server error", 500)
		}
	}()
	if r.Method == http.MethodGet && r.URL.Path == "/" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, chatHTML)
		return
	}
	if r.Method == http.MethodPost && r.URL.Path == "/chat" {
		s.handleChat(w, r)
		return
	}
	if r.URL.Path == "/tick" {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		s.handleTick(w, r)
		return
	}
	http.NotFound(w, r)
}

// tickPrompt is the autonomous health-check prompt fed to Emily when an
// external scheduler hits /tick. It must instruct her to publish an
// observation only when something is actually wrong, to avoid trampling
// over an existing open finding.
const tickPrompt = `Do an unattended health sweep of the fatbaby pipeline. ` +
	`First call fatbaby_read_observation to see if a finding is already open — if it is and still applies, do nothing. ` +
	`Otherwise: (1) call fatbaby_process_status to see which processes are running; ` +
	`(2) call fatbaby_signal_summary to check governance signal counts and recent alerts; ` +
	`(3) call fatbaby_eps_status to check EPS oracle precision; ` +
	`(4) tail logs for any running processes that show errors. ` +
	`If you find a real problem that needs source-code changes — parse errors, precision below 0.7, stalled processors, zero signal counts when filings exist — ` +
	`call fatbaby_write_observation with severity "error" or "warn" and a clear summary, findings, and suggested_fix. ` +
	`High/critical findings are automatically forwarded to Emily Prime's integration layer. ` +
	`If everything looks normal, reply "ok" and write nothing. Never write an observation for transient or self-inflicted state.`

func (s *Server) handleTick(w http.ResponseWriter, _ *http.Request) {
	// Refresh IDUNA agent token if within 5 minutes of expiry (spec §3.1).
	if idunaCfg, ok := loadIDUNAAgentCfg(); ok && !s.idunaTokenFresh() {
		if err := s.acquireIDUNAToken(idunaCfg); err != nil {
			log.Printf("IDUNA agent token: refresh failed: %v", err)
		}
	}
	msgs := []map[string]any{{"role": "user", "content": tickPrompt}}
	reply, calls := s.runToolLoop(msgs)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"reply": reply, "tool_calls": calls})
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Messages []map[string]any `json:"messages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	reply, calls := s.runToolLoop(req.Messages)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"reply": reply, "tool_calls": calls})
}

func (s *Server) runToolLoop(msgs []map[string]any) (string, []map[string]string) {
	toolCalls := []map[string]string{}
	for i := 0; i < s.cfg.MaxToolIters; i++ {
		s.limiter.Wait()
		payload := map[string]any{"model": "claude-sonnet-4-6", "max_tokens": 4096, "system": emilySystemPrompt, "tools": s.d.AnthropicDefs(), "messages": msgs}
		b, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPost, s.anthropicURL, bytes.NewReader(b))
		req.Header.Set("x-api-key", s.cfg.APIKey)
		req.Header.Set("anthropic-version", "2023-06-01")
		req.Header.Set("content-type", "application/json")
		resp, err := s.client.Do(req)
		if err != nil {
			log.Printf("anthropic_error err=%v", err)
			return "Anthropic request failed: " + err.Error(), toolCalls
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			log.Printf("anthropic_status status=%d body=%s", resp.StatusCode, string(body))
			return fmt.Sprintf("Anthropic API error: %s", strings.TrimSpace(string(body))), toolCalls
		}
		var ar struct {
			StopReason string           `json:"stop_reason"`
			Content    []map[string]any `json:"content"`
		}
		if err := json.Unmarshal(body, &ar); err != nil {
			return "Failed parsing Anthropic response", toolCalls
		}
		if ar.StopReason != "tool_use" {
			parts := []string{}
			for _, c := range ar.Content {
				if c["type"] == "text" {
					if t, ok := c["text"].(string); ok {
						parts = append(parts, t)
					}
				}
			}
			return strings.Join(parts, "\n"), toolCalls
		}
		msgs = append(msgs, map[string]any{"role": "assistant", "content": ar.Content})
		resultBlocks := []map[string]any{}
		for _, c := range ar.Content {
			if c["type"] != "tool_use" {
				continue
			}
			name, _ := c["name"].(string)
			id, _ := c["id"].(string)
			in, _ := c["input"].(map[string]any)
			fn := s.d.handlers[name]
			res := ""
			isErr := false
			if fn == nil {
				res = "unknown tool: " + name
				isErr = true
			} else {
				out, err := fn(in)
				res = out
				if err != nil {
					isErr = true
					if res == "" {
						res = err.Error()
					} else {
						res = res + "\n" + err.Error()
					}
				}
			}
			toolCalls = append(toolCalls, map[string]string{"tool": name, "result": res})
			resultBlocks = append(resultBlocks, map[string]any{"type": "tool_result", "tool_use_id": id, "content": res, "is_error": isErr})
		}
		msgs = append(msgs, map[string]any{"role": "user", "content": resultBlocks})
	}
	return "I reached the tool call limit without completing. Try again or simplify the request.", toolCalls
}

func main() {
	cfg := loadConfig()
	if cfg.APIKey == "" {
		log.Fatalf("FATAL: ANTHROPIC_API_KEY environment variable is not set.\nExport it before running: export ANTHROPIC_API_KEY=sk-ant-...")
	}
	s := NewServer(cfg)

	// Acquire IDUNA agent token at startup (spec HQ-SPEC-IAM-095 §3.1).
	if idunaCfg, ok := loadIDUNAAgentCfg(); ok {
		if err := s.acquireIDUNAToken(idunaCfg); err != nil {
			log.Printf("IDUNA agent token: initial acquire failed (%v) — will retry on next tick", err)
		} else {
			s.idunaMu.RLock()
			log.Printf("IDUNA agent token: acquired for %q (exp=%s)", idunaCfg.AgentName, s.idunaTokenExp.Format(time.RFC3339))
			s.idunaMu.RUnlock()
		}
	}

	// Build IDUNA IAM guard from IDUNA_JWKS_URL env or config/iam_config.json.
	// Falls back to no-op guard (all requests pass through) when unconfigured.
	guard, err := iamguard.NewFromEnv()
	if err != nil {
		log.Printf("iamguard: JWKS init failed (%v) — /chat and /tick will be unprotected", err)
		guard = &iamguard.Guard{}
	}
	if guard.IsActive() {
		log.Printf("iamguard: /chat protected (fatbaby.operator), /tick protected (governance.admin)")
	}

	mux := http.NewServeMux()
	mux.Handle("/", s)
	mux.Handle("/chat", guard.RequirePermission("fatbaby.operator")(s))
	mux.Handle("/tick", guard.RequirePermission("governance.admin")(s))
	log.Printf("emily-agent listening addr=:%s model=%s tools=%d fatbaby_root=%s", cfg.Port, "claude-sonnet-4-20250514", len(s.d.Defs()), cfg.FatbabyRoot)
	log.Fatal(http.ListenAndServe(":"+cfg.Port, mux))
}
