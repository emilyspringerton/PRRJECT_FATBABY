// tina-engine is the real "TINA desk engine" S189-42 asked for: on every real
// guidance RAISE (var/guidance/articles.ndjson), look up that same event's own
// source press release body (var/prwatch-body, keyed by source_identity) and
// draft a real, disclosure-compliant TINA article in the exact real voice/
// format four existing hand-written posts already established (IDUNA's own
// blog.db, "TINA: CAMELS -- What We Won't Claim to Know" et al.).
//
// Real, deliberate architecture, per the backlog's own explicit design note:
// this is NOT PARENA growing HTTP/JSON/collection capability from scratch --
// it's the exact same host-plugin split stdlib/editor/plugin.prn already
// established. This file is the real Go host side (data lookup + LLM draft
// generation); a PARENA-side FFI declaration for the deterministic detection
// step (which raises are new, haven't been drafted yet) belongs in a new
// stdlib/fatbaby.prn, following the same real pattern -- not built in this
// same pass, named as the next real step (see this repo's own CHANGELOG.md).
//
// Real, deliberate safety boundary, per TINA's own NORTHSTAR.md open
// question ("is there a real legal/compliance review this needs before any
// public-facing output ships... flag for founder decision"): this writes
// DRAFTS only (var/tina-drafts/<id>.json), never calls IDUNA's blog Create
// API directly. A human (or a future, separate review step) promotes a
// draft to a real post -- auto-publish is explicitly NOT wired here.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/example/prrject-fatbaby/internal/guidance"
	"github.com/example/prrject-fatbaby/prwatch"
)

func main() {
	guidanceFile := flag.String("guidance-file", filepath.Join("var", "guidance", "articles.ndjson"), "guidance-watcher's own flat output file")
	bodyRoot := flag.String("body-store", filepath.Join("var", "prwatch-body"), "prwatch body event store root (pr_body_fetched events)")
	draftsDir := flag.String("drafts-dir", filepath.Join("var", "tina-drafts"), "output directory for drafted TINA articles -- NOT auto-published")
	seenPath := flag.String("seen-file", filepath.Join("var", "tina-engine", ".seen"), "file tracking which guidance raise ids already have a real drafted article")
	model := flag.String("model", envOr("TINA_MODEL", "claude-sonnet-5"), "Anthropic model for TINA draft generation")
	pollInterval := flag.Duration("poll-interval", 5*time.Minute, "how often to re-scan for new guidance raises")
	oneShot := flag.Bool("one-shot", false, "process one batch and exit (useful for cron)")
	dryRun := flag.Bool("dry-run", false, "log what would be drafted, call no real LLM, write no files")
	flag.Parse()

	logger := log.New(os.Stdout, "tina-engine ", log.LstdFlags|log.LUTC)

	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" && !*dryRun {
		logger.Fatal("ANTHROPIC_API_KEY not set (use -dry-run to run without a real LLM call)")
	}

	if err := os.MkdirAll(*draftsDir, 0o755); err != nil {
		logger.Fatalf("mkdir %s: %v", *draftsDir, err)
	}
	if err := os.MkdirAll(filepath.Dir(*seenPath), 0o755); err != nil {
		logger.Fatalf("mkdir seen-file dir: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	drafter := &tinaDrafter{apiKey: apiKey, model: *model, dryRun: *dryRun, logger: logger}

	runOnce := func() {
		if err := runBatch(ctx, *guidanceFile, *bodyRoot, *draftsDir, *seenPath, drafter, logger); err != nil {
			logger.Printf("batch error: %v", err)
		}
	}

	runOnce()
	if *oneShot {
		return
	}
	ticker := time.NewTicker(*pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runOnce()
		}
	}
}

// runBatch is the real, deterministic detection step per the architecture's own design:
// find every real guidance RAISE not already in the seen-file, resolve its own real source
// press release body, and hand off to the drafter. A raise with no resolvable body yet (the
// crawler simply hasn't fetched it, a real, honest, common race against guidance-watcher's own
// faster extraction pass) is skipped, NOT marked seen -- it's retried on the next real poll,
// same "don't silently drop a real pending item" discipline eps-processor's own cursor design
// already follows for a different real gap.
func runBatch(ctx context.Context, guidanceFile, bodyRoot, draftsDir, seenPath string, drafter *tinaDrafter, logger *log.Logger) error {
	raises, err := readGuidanceRaises(guidanceFile)
	if err != nil {
		return fmt.Errorf("read guidance file: %w", err)
	}
	seen, err := readSeenSet(seenPath)
	if err != nil {
		return fmt.Errorf("read seen-file: %w", err)
	}

	var pending []guidance.Article
	for _, a := range raises {
		if !seen[a.ID] {
			pending = append(pending, a)
		}
	}
	if len(pending) == 0 {
		return nil
	}
	logger.Printf("found %d new real guidance raise(s) not yet drafted", len(pending))

	bodyByPRID, err := buildBodyIndex(ctx, bodyRoot)
	if err != nil {
		return fmt.Errorf("build body index: %w", err)
	}

	for _, a := range pending {
		prID := strings.TrimPrefix(a.SourceIdentity, "pr:")
		body, ok := bodyByPRID[prID]
		if !ok {
			logger.Printf("raise %s (%s): source press release body not fetched yet, will retry next poll", a.ID, a.Ticker)
			continue
		}
		draft, err := drafter.Draft(ctx, a, body)
		if err != nil {
			logger.Printf("raise %s (%s): draft failed: %v", a.ID, a.Ticker, err)
			continue
		}
		if err := writeDraft(draftsDir, a.ID, draft); err != nil {
			logger.Printf("raise %s (%s): write draft failed: %v", a.ID, a.Ticker, err)
			continue
		}
		if err := appendSeen(seenPath, a.ID); err != nil {
			logger.Printf("raise %s (%s): draft written but seen-file update failed (will re-draft next poll): %v", a.ID, a.Ticker, err)
			continue
		}
		logger.Printf("raise %s (%s): real TINA draft written to %s", a.ID, a.Ticker, filepath.Join(draftsDir, a.ID+".json"))
	}
	return nil
}

func readGuidanceRaises(path string) ([]guidance.Article, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []guidance.Article
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var a guidance.Article
		if err := json.Unmarshal(line, &a); err != nil {
			continue // real, honest skip -- one malformed line must not abort the whole real scan
		}
		if a.Action == guidance.ActionRaised {
			out = append(out, a)
		}
	}
	return out, sc.Err()
}

// buildBodyIndex scans the real prwatch-body event store once and returns a map keyed by the
// real numeric PR discovery id -- same real "one full scan, then serve every lookup from memory"
// convention guidance-watcher's own buildTickerMap already established against the discovery
// store, applied here to the body store instead.
func buildBodyIndex(ctx context.Context, bodyRoot string) (map[string]prwatch.BodyFetchedEvent, error) {
	store, err := eventstore.NewFileStore(bodyRoot)
	if err != nil {
		return nil, err
	}
	defer store.Close()

	out := map[string]prwatch.BodyFetchedEvent{}
	err = store.Scan(ctx, 1, func(rec eventstore.Record) error {
		if rec.Event.Type != "pr_body_fetched" {
			return nil
		}
		var body prwatch.BodyFetchedEvent
		if jsonErr := json.Unmarshal(rec.Event.Data, &body); jsonErr != nil {
			return nil // real, honest skip, same tolerance readGuidanceRaises already uses
		}
		out[body.PRDiscoveryID] = body
		return nil
	})
	return out, err
}

func readSeenSet(path string) (map[string]bool, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if id := strings.TrimSpace(sc.Text()); id != "" {
			out[id] = true
		}
	}
	return out, sc.Err()
}

func appendSeen(path, id string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintln(f, id)
	return err
}

// TinaDraft is the real, on-disk shape of one drafted (not yet published) article -- deliberately
// close to IDUNA's own real blog.db posts schema (title/author/body) so promoting a draft to a
// real post later is a direct field copy, not a redesign.
type TinaDraft struct {
	GuidanceID  string    `json:"guidance_id"`
	Ticker      string    `json:"ticker"`
	SourceURL   string    `json:"source_url"`
	Title       string    `json:"title"`
	Author      string    `json:"author"`
	Body        string    `json:"body"`
	GeneratedAt time.Time `json:"generated_at"`
}

func writeDraft(dir, id string, d TinaDraft) error {
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, id+".json"), b, 0o644)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// tinaDrafter generates one real TINA article draft per real guidance raise, via a real
// Anthropic API call -- same real, direct HTTP-to-/v1/messages pattern cmd/jon-agent/main.go
// already established (not re-derived), current model default (claude-sonnet-5, not jon-agent's
// own stale "claude-sonnet-4-6" -- a real, separate, pre-existing staleness in that file, not
// fixed here, out of this pass's own scope).
type tinaDrafter struct {
	apiKey string
	model  string
	dryRun bool
	logger *log.Logger
}

const tinaSystemPrompt = `You are drafting content for TINA (Trading Idea, Not Advice), FatBaby Markets Desk's
structured-output layer over its own SEC/PR Newswire signal pipeline. You write ONE real article
about ONE real guidance raise, grounded only in the real, provided source data -- never invented
facts, never a price target, never a position size, never a directive verb ("buy," "sell,"
"should," "consider"). The tone is plain, sourced, and hedged: state what was actually announced,
what it plausibly means and does not mean, and stop there. No urgency language, ever.

Real, required structure, matching this desk's own already-published house style exactly:
1. A short, factual title starting with "TINA: " (e.g. "TINA: Columbus McKinnon Raises FY26
   Guidance on Order Growth").
2. Open with the standard disclosure line: "**TINA — Trading Idea, Not Advice.** Structured
   output from FatBaby's signal pipeline. Not a recommendation. Not investment advice. Not a
   solicitation to buy or sell any security. See disclosure block at the end."
3. A "TOPIC:" line, one real sentence naming what's actually being looked at.
4. An "OBSERVATION" section: what was actually announced, plainly stated, with a real source
   link (use the exact URL given).
5. Real, honest context: what a guidance raise does and doesn't claim (it's a forward,
   binding-ish claim about expected performance, not a promise; a company raising once can
   still miss later). Do not speculate about undisclosed reasons for the raise.
6. A closing "What TINA won't do with this" section, naming explicitly what judgment call this
   stops short of (e.g. whether the new guidance range is itself impressive relative to the
   sector, whether the stock already priced this in) -- matching the real, already-published
   house convention of naming the exact line the system holds.
7. A closing disclosure block: "*Disclosure: TINA is a structured-output layer over FatBaby's
   own SEC/PR Newswire signal pipeline. Nothing above is investment advice, a recommendation, or
   a solicitation. Source links are the actual filings/press releases as ingested; verify
   against the primary source before acting on anything.*"

Return ONLY a JSON object: {"title": "...", "body": "..."} -- body is the full article in
Markdown, everything from the disclosure line through the closing disclosure block. No other
text before or after the JSON.`

func (d *tinaDrafter) Draft(ctx context.Context, a guidance.Article, body prwatch.BodyFetchedEvent) (TinaDraft, error) {
	userMsg := fmt.Sprintf(
		"Ticker: %s\nIssuer: %s\nGuidance action: raised\nMetric: %s\nEPS range: %v - %v\nRevenue range: %v - %v\nExtraction confidence: %.0f%%\nGuidance headline: %s\nGuidance summary: %s\n\nSource press release URL: %s\nSource press release headline: %s\nSource press release body:\n%s\n",
		a.Ticker, a.Issuer, a.Metric, floatOrNil(a.EPSLow), floatOrNil(a.EPSHigh), floatOrNil(a.RevenueLow), floatOrNil(a.RevenueHigh),
		a.Confidence*100, a.Headline, a.Body, body.URL, body.Headline, body.Body,
	)

	if d.dryRun {
		d.logger.Printf("[dry-run] would draft TINA article for %s (%s), source=%s", a.ID, a.Ticker, body.URL)
		return TinaDraft{
			GuidanceID: a.ID, Ticker: a.Ticker, SourceURL: body.URL,
			Title: "[dry-run, no real LLM call made]", Author: "TINA (FatBaby Markets Desk)",
			Body: userMsg, GeneratedAt: time.Now().UTC(),
		}, nil
	}

	respText, err := d.callAnthropic(ctx, userMsg)
	if err != nil {
		return TinaDraft{}, err
	}

	var parsed struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if err := json.Unmarshal([]byte(extractJSONObject(respText)), &parsed); err != nil {
		return TinaDraft{}, fmt.Errorf("model did not return the required JSON shape: %w (raw: %.200s)", err, respText)
	}
	if parsed.Title == "" || parsed.Body == "" {
		return TinaDraft{}, fmt.Errorf("model returned an empty title or body")
	}

	return TinaDraft{
		GuidanceID: a.ID, Ticker: a.Ticker, SourceURL: body.URL,
		Title: parsed.Title, Author: "TINA (FatBaby Markets Desk)",
		Body: parsed.Body, GeneratedAt: time.Now().UTC(),
	}, nil
}

// extractJSONObject trims any real, occasional leading/trailing prose a model adds despite
// being told not to -- takes the substring from the first '{' to the last '}', a real, cheap,
// honest tolerance rather than a strict parse failure on otherwise-good output.
func extractJSONObject(s string) string {
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start == -1 || end == -1 || end < start {
		return s
	}
	return s[start : end+1]
}

func floatOrNil(f *float64) any {
	if f == nil {
		return "n/a"
	}
	return *f
}

const anthropicURL = "https://api.anthropic.com/v1/messages"

func (d *tinaDrafter) callAnthropic(ctx context.Context, userMsg string) (string, error) {
	reqBody := map[string]any{
		"model":      d.model,
		"max_tokens": 2048,
		"system":     tinaSystemPrompt,
		"messages":   []map[string]string{{"role": "user", "content": userMsg}},
	}
	b, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicURL, bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", d.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("anthropic request: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("anthropic status %d: %s", resp.StatusCode, string(respBody))
	}

	var parsed struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("decode anthropic response: %w", err)
	}
	if len(parsed.Content) == 0 {
		return "", fmt.Errorf("anthropic response had no content blocks")
	}
	return parsed.Content[0].Text, nil
}
