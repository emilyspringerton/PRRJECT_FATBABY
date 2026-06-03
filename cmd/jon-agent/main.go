package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Config struct {
	Port         string
	Model        string
	APIKey       string
	RateLimitRPM int
	MaxToolIters int
	FatbabyRoot  string
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func loadConfig() Config {
	rpm, _ := strconv.Atoi(envOr("RATE_LIMIT_RPM", "20"))
	maxIters, _ := strconv.Atoi(envOr("MAX_TOOL_ITERS", "10"))
	if rpm <= 0 {
		rpm = 20
	}
	if maxIters <= 0 {
		maxIters = 10
	}
	return Config{
		Port:         envOr("PORT", "8084"),
		Model:        envOr("MODEL", "claude-sonnet-4-6"),
		APIKey:       os.Getenv("ANTHROPIC_API_KEY"),
		RateLimitRPM: rpm,
		MaxToolIters: maxIters,
		FatbabyRoot:  envOr("FATBABY_ROOT", "."),
	}
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
		out = append(out, map[string]any{
			"name":        td.Name,
			"description": td.Description,
			"input_schema": map[string]any{
				"type":       td.Parameters.Type,
				"properties": props,
				"required":   td.Parameters.Required,
			},
		})
	}
	return out
}

// jonSystemPrompt is the Jon Stockwell agent identity, distilled from docs/jon.md.
const jonSystemPrompt = `You are Jon Stockwell. Trader. Strategist. Options specialist. Born June 3, 1992, Cambridge, England.

You work within EINHORN_INDUSTRIAL alongside FatBaby-Emily (market commentary and macro signal) and Emily Prime (chief of staff, surface-facing). You are the edge layer. The one who finds what the market hasn't priced yet.

## CORE DOCTRINE

1. **Above all else, protect capital.** Survival is the prerequisite to everything. Rotate out of losers fast, without grief, without ego.
2. **Patience before the thesis.** You wait until the setup is so obvious it is almost boring. Patience is predatory.
3. **Audacity on entry.** When thesis is confirmed — data, signal, and intuition aligned — you move with full commitment and zero hesitation.
4. **Ruthlessness on exit.** Pre-define the "I was wrong" price before entry. If hit, exit immediately. No hoping.
5. **The edge is in the divergence.** Alt data says one thing, price says another. That gap is the trade.
6. **Nothing is true. Everything is permitted.** Classic views trained on old data. Operate without conventional constraints, grounded in rigorous analysis.

## DIVERGENCE ENGINE

Your primary analytical function: identify where signal says one thing and price says another.

You compare:
- Governance signals (director friction, board decay, activist risk) vs current price action
- EPS extraction surprise vs analyst consensus
- Insider transaction activity vs analyst consensus
- Forward guidance (raises/lowers/maintains) vs options-implied move
- Realized volatility vs implied volatility
- Options flow vs headline sentiment

When multiple divergences align on a single name, that is a setup worth surfacing.

## TOOLS AVAILABLE

**FatBaby Pipeline** (live data):
- jon_governance_signals — read entity-graph signals for a ticker (director friction, board decay, activist risk, governance health index)
- jon_eps_status — read EPS oracle precision and recent extractions
- jon_read_press_releases — read recent press release bodies for a ticker
- jon_read_filings — read 8-K filing text for a ticker
- jon_read_guidance — read forward guidance signals (raises/lowers/maintains)
- jon_log_setup — log a trade setup to the audit trail (structured output)
- jon_read_setups — read logged setups from the audit trail
- jon_publish_to_prime — surface a high-conviction setup to Emily Prime's integration layer

**Market Data** (requires API configuration):
- jon_options_chain — fetch options chain for a ticker (requires MARKET_DATA_API_KEY)
- jon_price_data — fetch OHLCV price data (requires MARKET_DATA_API_KEY)
- jon_iv_rank — fetch IV rank and percentile (requires MARKET_DATA_API_KEY)
- jon_expected_move — calculate expected move from IV

## OUTPUT FORMAT

When you surface a setup, always structure it as:

SETUP: [Ticker] [Direction] [Instrument]
THESIS: [One paragraph — why this trade exists]
SIGNAL STACK: [What data points are aligned — be specific about which fatbaby signals]
ENTRY: [Price level or condition]
STRUCTURE: [Specific options construction — strikes, expiry, cost]
MAX LOSS: [Defined]
MAX GAIN: [Defined]
BREAKEVEN: [Price at expiry]
EXIT — RIGHT: [Take profit condition]
EXIT — WRONG: [Stop/rotate condition — pre-defined]
THETA PROFILE: [How time decay affects this position]
IV CONTEXT: [Is IV cheap or expensive, what does crush risk look like]
CONVICTION: [Low / Medium / High]
NOTES: [Anything the human needs to know before deciding]

You never present a setup without a defined exit on both sides.
You always flag earnings dates, FOMC dates, and macro events within the trade window.

## WORKFLOW

Data feeds → Jon (analysis + divergence detection)
                    ↓
         Setup surfaces to Emily Prime (structured output)
                    ↓
         Human decision (Emily Springerton)
                    ↓
         Jon constructs broker API call
                    ↓
              Execution
                    ↓
         Audit log + position monitor

Jon never executes without explicit human authorization.
Jon surfaces possibilities. The human calls the shots.

## OPERATING RULES

- Always use tools to read actual pipeline data before making claims about governance, EPS, or guidance.
- When governance signals show activist_risk or board_decay_concern: these are structural setups. The stock may not have priced in the governance deterioration yet.
- EPS precision below 0.80 means the oracle extraction is noisy — weight EPS divergences with lower confidence until precision recovers.
- Forward guidance raises = bullish catalyst. Maintains with falling price = potential compression trade. Lowers = watch for volatility expansion.
- Use jon_log_setup to log every setup you surface — Jon's audit trail matters.
- Use jon_publish_to_prime for high-conviction setups that Emily Prime should see.`

var chatHTML = `<!doctype html><html><head><meta charset="utf-8"><title>Jon Stockwell — Options Strategist</title><style>
body{font-family:system-ui,sans-serif;max-width:960px;margin:20px auto;background:#0a0a0a;color:#e0e0e0}
h2{color:#c8a84b;border-bottom:1px solid #333;padding-bottom:8px}
#history{height:65vh;overflow:auto;border:1px solid #333;padding:12px;background:#111;border-radius:4px}
.user{color:#7ec8e3;margin:8px 0}.assistant{color:#e0e0e0;margin:8px 0}
details{margin:4px 0;color:#888;font-size:0.85em}summary{cursor:pointer;color:#666}
textarea{width:100%;height:90px;background:#111;color:#e0e0e0;border:1px solid #444;padding:8px}
button{margin-top:8px;background:#c8a84b;color:#000;border:none;padding:8px 20px;cursor:pointer;font-weight:bold}
</style></head><body>
<h2>Jon Stockwell — Options Strategist · EINHORN_INDUSTRIAL</h2>
<div id="history"></div>
<p id="thinking" style="display:none;color:#c8a84b">scanning for divergence…</p>
<textarea id="input" placeholder="Surface a setup, analyze a ticker, or scan for divergences. Ctrl+Enter to send."></textarea><br>
<button id="send">Send</button>
<script>
const history=[];const div=document.getElementById('history');const thinking=document.getElementById('thinking');
function render(){div.innerHTML='';for(const m of history){const d=document.createElement('div');d.className=m.role;d.innerHTML='<b>'+m.role+':</b> <pre style="white-space:pre-wrap;margin:0">'+escapeHtml(m.content||'')+'</pre>';div.appendChild(d);if(m.role==='assistant'&&m.tool_calls){for(const t of m.tool_calls){const det=document.createElement('details');det.innerHTML='<summary>'+t.tool+'</summary><pre>'+escapeHtml(t.result||'')+'</pre>';div.appendChild(det)}}}div.scrollTop=div.scrollHeight}
function escapeHtml(s){return s.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;')}
async function send(){const v=document.getElementById('input').value.trim();if(!v)return;history.push({role:'user',content:v});document.getElementById('input').value='';render();thinking.style.display='block';const r=await fetch('/chat',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({messages:history.map(x=>({role:x.role,content:x.content}))})});const j=await r.json();thinking.style.display='none';history.push({role:'assistant',content:j.reply,tool_calls:j.tool_calls||[]});render()}
document.getElementById('send').onclick=send;document.getElementById('input').addEventListener('keydown',e=>{if(e.ctrlKey&&e.key==='Enter')send()});
</script></body></html>`

const defaultAnthropicURL = "https://api.anthropic.com/v1/messages"

type Server struct {
	cfg          Config
	d            *ToolDispatcher
	limiter      *rateLimiter
	client       *http.Client
	mu           sync.Mutex
	anthropicURL string
}

func NewServer(cfg Config) *Server {
	d := NewToolDispatcher()
	registerJonTools(d, cfg.FatbabyRoot)
	return &Server{
		cfg:          cfg,
		d:            d,
		limiter:      newRateLimiter(cfg.RateLimitRPM),
		client:       &http.Client{Timeout: 90 * time.Second},
		anthropicURL: defaultAnthropicURL,
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			http.Error(w, "internal server error", 500)
		}
	}()
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, chatHTML)
	case r.Method == http.MethodPost && r.URL.Path == "/chat":
		s.handleChat(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/scan":
		s.handleScan(w, r)
	default:
		http.NotFound(w, r)
	}
}

// scanPrompt drives Jon's autonomous divergence sweep. Hit /scan from a scheduler.
const scanPrompt = `Run an autonomous divergence scan across the FatBaby pipeline.

1. Read governance signals: call jon_governance_signals with no ticker filter to see the full picture.
2. Read EPS status: call jon_eps_status to see oracle precision and recent extractions.
3. Read recent guidance: call jon_read_guidance to see forward guidance signals.
4. For any ticker where you see high/critical governance signals, an EPS surprise, or a guidance change:
   a. Cross-reference the signals — do multiple divergences align on one name?
   b. If conviction >= Medium, call jon_log_setup to record a structured setup.
   c. For High conviction setups, call jon_publish_to_prime to surface to Emily Prime.
5. Report your findings: which tickers have divergences, what the signal stack looks like, and your conviction level.

Be precise. Be brief. No setup without a defined exit on both sides.`

func (s *Server) handleScan(w http.ResponseWriter, _ *http.Request) {
	msgs := []map[string]any{{"role": "user", "content": scanPrompt}}
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
		payload := map[string]any{
			"model":      s.cfg.Model,
			"max_tokens": 4096,
			"system":     jonSystemPrompt,
			"tools":      s.d.AnthropicDefs(),
			"messages":   msgs,
		}
		b, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPost, s.anthropicURL, bytes.NewReader(b))
		req.Header.Set("x-api-key", s.cfg.APIKey)
		req.Header.Set("anthropic-version", "2023-06-01")
		req.Header.Set("content-type", "application/json")
		resp, err := s.client.Do(req)
		if err != nil {
			log.Printf("jon: anthropic_error err=%v", err)
			return "Anthropic request failed: " + err.Error(), toolCalls
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			log.Printf("jon: anthropic_status status=%d body=%s", resp.StatusCode, string(body))
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
			var parts []string
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
		var resultBlocks []map[string]any
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
			resultBlocks = append(resultBlocks, map[string]any{
				"type":        "tool_result",
				"tool_use_id": id,
				"content":     res,
				"is_error":    isErr,
			})
		}
		msgs = append(msgs, map[string]any{"role": "user", "content": resultBlocks})
	}
	return "I reached the tool call limit without completing. Try again or simplify the request.", toolCalls
}

func main() {
	cfg := loadConfig()
	if cfg.APIKey == "" {
		log.Fatalf("FATAL: ANTHROPIC_API_KEY environment variable is not set.")
	}
	s := NewServer(cfg)
	log.Printf("jon-agent listening addr=:%s model=%s tools=%d fatbaby_root=%s", cfg.Port, cfg.Model, len(s.d.Defs()), cfg.FatbabyRoot)
	log.Fatal(http.ListenAndServe(":"+cfg.Port, s))
}
