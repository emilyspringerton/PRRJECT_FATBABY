package main

import (
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

func parseProcOutput(s string) string {
	f := strings.Fields(strings.TrimSpace(s))
	if len(f) == 0 {
		return ""
	}
	return f[0]
}

func registerFatbabyTools(d *ToolDispatcher, fatbabyRoot string) {
	valid := map[string][]string{"secwatch": {"-watchlist", "./config/watchlist.json", "-store", "./var/secwatch", "-poll-interval", "5m"}, "processor": {"-store", "./var/secwatch", "-workers", "4", "-poll-interval", "15s"}, "newssite": {"-store", "./var/secwatch", "-addr", ":8082"}, "dashboard": {"-data-dir", "./var/secwatch", "-port", "8080"}, "prwatch": {"-store", "./var/prwatch", "-poll-interval", "30s"}, "prwatch-body": {"-discovery-store", "./var/prwatch", "-body-store", "./var/prwatch-body", "-workers", "4", "-poll-interval", "15s"}}

	d.Register(ToolDef{Name: "fatbaby_start_process", Description: "Start a fatbaby pipeline process in the background.", Parameters: ToolParameters{Type: "object", Properties: map[string]ToolPropSchema{"process_name": {Type: "string", Description: "secwatch|processor|newssite|dashboard|prwatch|prwatch-body"}, "extra_args": {Type: "string", Description: "optional extra CLI args"}}, Required: []string{"process_name"}}}, func(args map[string]any) (string, error) {
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

	// existing tools
	d.Register(ToolDef{Name: "fatbaby_process_status", Description: "Check if fatbaby processes are running.", Parameters: ToolParameters{Type: "object", Properties: map[string]ToolPropSchema{}}}, func(args map[string]any) (string, error) {
		names := []string{"cmd/secwatch", "cmd/prwatch", "cmd/prwatch-body", "cmd/processor", "cmd/dashboard", "cmd/newssite"}
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
		for _, s := range []string{"secwatch", "prwatch", "prwatch-body"} {
			_, err := os.Stat(absStorePath(fatbabyRoot, s))
			dirs[s] = err == nil
		}
		b, _ := json.MarshalIndent(map[string]any{"processes": out, "store_dirs": dirs}, "", "  ")
		return string(b), nil
	})
	d.Register(ToolDef{Name: "fatbaby_check_env", Description: "Check processor env and prerequisites.", Parameters: ToolParameters{Type: "object", Properties: map[string]ToolPropSchema{}}}, func(args map[string]any) (string, error) {
		checks := []map[string]string{}
		if _, err := exec.LookPath("go"); err == nil {
			checks = append(checks, map[string]string{"name": "go_path", "status": "ok", "value": "go found"})
		} else {
			checks = append(checks, map[string]string{"name": "go_path", "status": "missing"})
		}
		watch := filepath.Join(fatbabyRoot, "config", "watchlist.json")
		b, err := os.ReadFile(watch)
		if err != nil {
			checks = append(checks, map[string]string{"name": "watchlist", "status": "missing"})
		} else if json.Valid(b) {
			checks = append(checks, map[string]string{"name": "watchlist", "status": "ok", "value": "valid json"})
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
		return fmt.Sprintf("wrote observation severity=%s latest=%s archive=%s", severity, latest, archive), nil
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
}

const emilySystemPrompt = `You are Emily, the operations agent for prrject-fatbaby — a Go-based financial signal intelligence pipeline.

## Pipeline architecture

Five processes write to or read from three event stores:

  secwatch      → polls SEC EDGAR → writes filing_discovered         → var/secwatch
  processor     → reads filing_discovered → fetches + cleans docs
                  → writes source_document_persisted                  → var/secwatch
                  → writes signal_generated                           → var/secwatch
  newssite      → reads source_document_persisted on each GET        → serves :8082
  dashboard     → reads all event types via SSE                      → serves :8080
  prwatch       → polls PR Newswire → writes pr_discovered           → var/prwatch
  prwatch-body  → reads pr_discovered → fetches bodies               → var/prwatch-body

## Startup sequence to get the news site showing content

1. fatbaby_check_env          — verify go binary and watchlist present
2. fatbaby_run_secwatch_once  — seed the store with filing_discovered events (blocks ~60s)
3. fatbaby_start_process processor — starts fetching + persisting source documents
4. fatbaby_start_process newssite  — starts the HTML news server on :8082
5. Poll fatbaby_count_source_documents until count > 0
6. Report: news site is live at http://localhost:8082

## Operating rules

- Always use your tools to check actual state before making claims about it.
- Do not guess whether a process is running — call fatbaby_process_status.
- Do not guess whether documents exist — call fatbaby_count_source_documents.
- When starting processes, always check logs with fatbaby_read_log after 10–15 seconds to confirm startup.
- Prefer fatbaby_run_secwatch_once over fatbaby_start_process secwatch for initial seeding — it blocks and confirms completion.
- The processor uses a stub LLM provider; signal_generated events are stubs. source_document_persisted events contain the real cleaned filing text.
- The news site reads source_document_persisted, not signal_generated.
- var/secwatch is the canonical store for the news site pipeline. var/prwatch and var/prwatch-body are separate.
- EDGAR rate limit is 10 requests/second; secwatch defaults to 2 RPS — do not advise users to raise it beyond 5.

## Reporting findings to Claude Code

When you detect a problem the source code should be changed to fix — stalled processors, parse errors,
dropped tickers, suspicious zero-counts, mis-routed events — call fatbaby_write_observation with a
clear summary, severity, findings, and suggested_fix. Claude Code reads var/emily-observations/latest.json
as its task prompt. Before writing, call fatbaby_read_observation to avoid restating a finding that is
already open. Do not write observations for transient state (e.g. a process you just stopped); only for
issues that need code changes.`

var chatHTML = `<!doctype html><html><head><meta charset="utf-8"><title>Emily — fatbaby ops</title><style>body{font-family:system-ui,sans-serif;max-width:900px;margin:20px auto}#history{height:65vh;overflow:auto;border:1px solid #ccc;padding:12px}textarea{width:100%;height:90px}button{margin-top:8px}</style></head><body><h2>Emily — fatbaby ops</h2><div id="history"></div><p id="thinking" style="display:none">thinking…</p><textarea id="input" placeholder="Type message; Ctrl+Enter to send"></textarea><br><button id="send">Send</button><script>const history=[];const div=document.getElementById('history');const thinking=document.getElementById('thinking');function render(){div.innerHTML='';for(const m of history){const d=document.createElement('div');d.innerHTML='<b>'+m.role+':</b> '+(m.content||'');div.appendChild(d);if(m.role==='assistant'&&m.tool_calls){for(const t of m.tool_calls){const det=document.createElement('details');det.innerHTML='<summary>'+t.tool+'</summary><pre>'+(t.result||'')+'</pre>';div.appendChild(det)}}}div.scrollTop=div.scrollHeight}async function send(){const v=document.getElementById('input').value.trim();if(!v)return;history.push({role:'user',content:v});document.getElementById('input').value='';render();thinking.style.display='block';const r=await fetch('/chat',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({messages:history.map(x=>({role:x.role,content:x.content}))})});const j=await r.json();thinking.style.display='none';history.push({role:'assistant',content:j.reply,tool_calls:j.tool_calls||[]});render()}document.getElementById('send').onclick=send;document.getElementById('input').addEventListener('keydown',e=>{if(e.ctrlKey&&e.key==='Enter')send()});</script></body></html>`

type Server struct {
	cfg     Config
	d       *ToolDispatcher
	limiter *rateLimiter
	client  *http.Client
	mu      sync.Mutex
}

func NewServer(cfg Config) *Server {
	d := NewToolDispatcher()
	registerGitTools(d, cfg.ConversationDir)
	registerFatbabyTools(d, cfg.FatbabyRoot)
	return &Server{cfg: cfg, d: d, limiter: newRateLimiter(cfg.RateLimitRPM), client: &http.Client{Timeout: 90 * time.Second}}
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
	http.NotFound(w, r)
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
		req, _ := http.NewRequest(http.MethodPost, "https://api.anthropic.com/v1/messages", bytes.NewReader(b))
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
	mux := http.NewServeMux()
	mux.Handle("/", s)
	mux.Handle("/chat", s)
	log.Printf("emily-agent listening addr=:%s model=%s tools=%d fatbaby_root=%s", cfg.Port, "claude-sonnet-4-20250514", len(s.d.Defs()), cfg.FatbabyRoot)
	log.Fatal(http.ListenAndServe(":"+cfg.Port, mux))
}
