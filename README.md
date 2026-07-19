# prrject-fatbaby

A Go-based financial signal intelligence pipeline that's grown into a small operating system for turning SEC EDGAR filings and PR Newswire press releases into structured, queryable, publicly-readable financial intelligence — governance signals, EPS results, insider transactions, dividend/buyback actions, market-wide movers, entity relationships — served through a public news site, a streaming TCP feed, an SSE dashboard, an HTTP signal API, and an LLM-powered operations agent (Emily) that can monitor the system and answer questions about what the signals mean.

At this point the value here isn't any one pipeline — it's the accumulated **data** (630MB+ and growing event store, 116K+ records, 50 tracked tickers, entity graph of directors/auditors/boards spanning 20+ years of filings) and the **patterns** the codebase has converged on for building each new collector/consumer without reinventing plumbing. See [Patterns](#patterns) below — that's the part worth reading before adding a 37th `cmd/` binary.

The original centerpiece is still a **recursive self-improving entity graph** that turns 8-K annual meeting filings into director-level governance intelligence — friction scores, activist risk composites, entrenchment patterns, and cross-company board relationships — and feeds anomalies back to Claude Code for automatic rule refinement. It's since been joined by a real public product surface (`cmd/newssite`) with per-ticker pages, own-hosted charts, a daily auto-generated "Stocks on the Move" article, and an editorial content pipeline, plus a growing set of specialized watchers (insider Form 4s, dividends, buybacks, NT late-filing notices, forward guidance, EPS reconciliation, earnings calendar).

---

## Environment variables

All variables are optional unless marked **Required**.

| Variable | Required | Default | Description |
|---|---|---|---|
| `ANTHROPIC_API_KEY` | **Required** | — | Anthropic API key; used by emily-agent and observation-watcher |
| `MODEL` | No | `claude-sonnet-4-6` | LLM model for emily-agent |
| `VALIDATOR_MODEL` | No | same as `MODEL` | Model used for RSI validation pass |
| `PORT` | No | `8080` | HTTP server port for emily-agent |
| `FATBABY_ROOT` | No | `.` | Project root directory; locates `var/` and `config/` |
| `RATE_LIMIT_RPM` | No | `60` | Requests-per-minute cap for emily-agent HTTP API |
| `MAX_TOOL_ITERS` | No | `20` | Maximum agentic tool loop iterations per request |
| `GIT_COMMIT` | No | — | Injected at build time (`-ldflags "-X main.GitCommit=$(git rev-parse --short HEAD)"`); shown in `/status` |
| `CONVERSATION_DIR` | No | `./emily-memory` | Directory for persisted conversation history |
| `EMILY_INTEGRATION_DIR` | No | — | Path to Emily Prime signals directory; enables Emily ↔ FatBaby integration |
| `EMILY_PRIME_TASKS_DIR` | No | `../EMILY/signals/tasks` | Path to Emily Prime directed tasks directory; auto-detected from sibling EMILY repo |
| `OBSERVATION_CMD` | No | `claude` | Executable invoked by observation-watcher for each new observation |
| `OBSERVATION_CMD_ARGS` | No | `--dangerously-skip-permissions` | Space-separated extra args passed to `OBSERVATION_CMD` |
| `OBSERVATION_GATE` | No | `nontrivial` | Gate mode: `none` (always invoke) or `nontrivial` (skip trivial high-trust-only batches) |
| `ENTITY_GRAPH_RULES` | No | `config/entity-graph-rules.json` | Path to entity-graph rules file; hot-reloaded on each batch |
| `GITHUB_TOKEN` | No | — | GitHub personal access token (`issues:write` scope); enables GitHub issue creation from observations |
| `GITHUB_OWNER` | No | — | GitHub org or user for issue creation (required when `GITHUB_TOKEN` is set) |
| `GITHUB_REPO` | No | — | GitHub repository name for issue creation (required when `GITHUB_TOKEN` is set) |
| `IDUNA_BASE_URL` | No | — | IDUNA IAM service base URL; enables M2M token acquisition for emily-agent |
| `IDUNA_AGENT_NAME` | No | — | Agent name for IDUNA M2M authentication |
| `IDUNA_AGENT_SECRET` | No | — | Agent secret for IDUNA M2M authentication |
| `IDUNA_JWKS_URL` | No | — | IDUNA JWKS URL for JWT verification in signalapi and dashboard |

---

## Architecture

The diagram below is the original RSI loop (entity-graph's recursive self-improvement) — still
real and still running, but no longer the whole picture. It doesn't show the specialized
watchers (Form 4, dividends, buybacks, NT filings, guidance, earnings calendar), the market-data/
movers pipeline, or newssite's full surface (ticker pages, charts, commentary, blog-adjacent
content). See [All processes](#all-processes) for the complete, current map.

```
SEC EDGAR          PR Newswire
    │                   │
    ▼                   ▼
 secwatch            prwatch ──► prwatch-body
    │
    ▼
 var/secwatch/  (event store)
    │
    ├──► processor      (filing text → signals)
    │
    ├──► entity-graph   (8-K Item 5.07 → director graph + governance signals)
    │         │
    │         ▼
    │    var/entity-graph/
    │    (nodes, edges, signals, auditors — NDJSON append-only)
    │         │
    │         ▼
    │    emily-observations/latest.json
    │         │
    │         ▼
    │    observation-watcher ──► claude --dangerously-skip-permissions
    │    (polls; gates trivial batches)   (refines rules or parser)
    │                                          │
    │                                          ▼
    │                             config/entity-graph-rules.json
    │                             (hot-reloaded; no restart needed)
    │
    ├──► dashboard      (SSE stream  :8080)
    ├──► newssite       (filing HTML :8082)
    ├──► feedserver     (TCP framed  :8083)
    ├──► signalapi      (HTTP query  :8084)
    └──► emily-agent    (LLM ops + signal analyst :8080)
```

All event stores are **file-backed append-only NDJSON**, UTC-date-partitioned, with monotonic sequence numbers. Processes resume from their cursor after restart.

## AI system governance

The agent operating model is documented in [`docs/ai-system-governance.md`](docs/ai-system-governance.md). It defines the three-agent boundary between Emiree, Emily Prime, and FatBaby-Emily; the allowed observation/directive/query channels; audit-trail requirements; and the improvement loops that keep the financial signal pipeline legible while it runs continuously.

---

## Entity-graph intelligence engine

The entity-graph pipeline (`cmd/entity-graph`) processes SEC 8-K filings containing Item 5.07 vote results from annual meetings and builds a persistent knowledge graph of directors, their approval histories, and their relationships across companies.

### What it extracts from each 8-K

- **Director nominees**: name (dotted initials, compound hyphenated surnames), for/against/abstain/broker-non-vote counts, approval %
- **Non-director proposals**: supermajority thresholds, pass/fail outcomes, vote fractions
- **Auditor**: public accounting firm from the ratification proposal

### Signal types

| Signal | Severity | What it means |
|--------|----------|---------------|
| `director_friction` | medium–high | Approval below 85% — activist targeting or board misalignment |
| `nomination_rejection` | critical | Approval below 50% — director must submit resignation under majority-voting standards |
| `director_decay` | low–medium | Approval declining year-over-year — replacement likely within 12–18 months |
| `high_trust_director` | low | Approval above 95% — stable seat |
| `governance_entrenchment` | high | Proposal passed by large majority but blocked by supermajority-of-outstanding threshold — structural M&A defense |
| `activist_risk` | high | **Composite**: entrenchment + friction co-occur within 12 months. Base rate: activist 13D within 6 months in ~60% of cases |
| `director_link` | low | Friction director sits on multiple boards — risk propagates across portfolio |
| `family_control` | medium | Director name matches founder/family keyword — concentrated founder control |
| `broker_nonvote_anomaly` | low | Broker non-votes above 12% — elevated retail/street-name voting |
| `compensation_concern` | medium | Say-on-pay opposition above 30% — ESG funds agitating on executive pay |
| `abstention_spike` | low | Proposal abstention above 10% — shareholder confusion or protest vote |
| `auditor_change` | medium | Company switched public accounting firm — may precede regulatory action or transaction |

Signal thresholds are **hot-reloadable** from `config/entity-graph-rules.json` — the recursive self-improvement loop edits this file and the running process picks up changes on the next batch without a restart.

### Recursive self-improvement loop

```
entity-graph process
  → publishes var/emily-observations/latest.json
  → observation-watcher detects content-hash change
  → (gate: skip if only high_trust signals fired, no gaps or errors)
  → builds prompt: observation JSON + current rules.json
  → invokes: claude --dangerously-skip-permissions "<prompt>"
  → Claude edits config/entity-graph-rules.json (thresholds)
       or internal/entitygraph/parser.go (parse failures)
  → runs go test ./...
  → commits passing changes
  → entity-graph hot-reloads rules on next batch
  → publishes updated observation → loop continues
```

```bash
# Terminal 1
go run ./cmd/entity-graph \
  -store ./var/secwatch \
  -graph-dir ./var/entity-graph \
  -obs-dir ./var/emily-observations

# Terminal 2
go run ./cmd/observation-watcher -gate nontrivial
```

---

## Emily agent

Emily (`cmd/emily-agent`) is a Claude-powered operations agent with two roles:

**Ops agent** — start/stop pipeline processes, tail logs, check health, count documents, write observations for Claude Code.

**Signal analyst** — answers questions about governance signals using live data from the entity-graph store:

- *"What's the risk picture for SCHW?"*
- *"Is there activist risk in our portfolio?"*
- *"Who are the directors at GS and what are their approval trends?"*
- *"Which directors sit on multiple boards?"*

| Tool | Purpose |
|------|---------|
| `fatbaby_signal_summary` | Dashboard: all signals by type/severity, top tickers, recent high/critical alerts |
| `fatbaby_query_signals` | Filtered search: ticker, signal type, min severity, date window |
| `fatbaby_entity_graph` | Director/company lookup: approval trends, co-board partners, auditor history |

```bash
export ANTHROPIC_API_KEY=sk-ant-...
go run ./cmd/emily-agent
# → http://localhost:8080
```

---

## Quick start

### Requirements

- Go 1.22+
- `ANTHROPIC_API_KEY`

```bash
go mod download
```

### 1. Seed SEC filings

```bash
go run ./cmd/secwatch \
  -watchlist ./config/watchlist.json \
  -store ./var/secwatch
```

### 2. Run entity-graph pipeline

```bash
go run ./cmd/entity-graph \
  -store ./var/secwatch \
  -graph-dir ./var/entity-graph \
  -obs-dir ./var/emily-observations
```

### 3. Start Emily

```bash
go run ./cmd/emily-agent   # → http://localhost:8080
```

### 4. (Optional) Recursive self-improvement

```bash
go run ./cmd/observation-watcher -gate nontrivial
```

---

## All processes

Grouped by role, not alphabetically — this is the map that matters when you're trying to figure
out what actually touches a given piece of data.

**Ingestion** — turn the outside world into events
| Process | Purpose |
|---------|---------|
| `cmd/secwatch` | Polls SEC EDGAR → `filing_discovered` + `source_document_persisted` events |
| `cmd/prwatch` | Polls PR Newswire → `pr_discovered` events |
| `cmd/prwatch-body` | Fetches press release bodies → `pr_body_fetched` events |
| `cmd/market-data-watcher` | Daily OHLCV bars (Yahoo, free) for every watchlist ticker — powers own-hosted charts, no third-party embeds |
| `cmd/movers-watcher` | Daily market-wide gainers/losers (Yahoo screener), gated to real trading days (`internal/marketcal`) — publishes "Stocks on the Move" |
| `cmd/form4-watcher` | SEC Form 4 insider transactions → `insider_buy`/`insider_sell_cluster` signals |
| `cmd/dividend-watcher` | Classifies dividend cuts/raises/specials from press-release bodies |
| `cmd/buyback-watcher` | Classifies buyback authorizations/suspensions from press-release bodies |
| `cmd/nt-watcher` | NT 10-K/10-Q late-filing notifications, classifies reason |
| `cmd/guidance-watcher` | Extracts forward guidance (raise/lower/maintain) from press releases |
| `cmd/earnings-calendar` | Builds the report-date calendar from three sources: EPS backfill, PR body extraction, 8-K Item 2.02 |
| `cmd/schd13-watcher` | Schedule 13D/G ownership filings |

**Processing** — turn events into structured signals and knowledge
| Process | Purpose |
|---------|---------|
| `cmd/processor` | Filing/press-release text → structured `signal_generated` events |
| `cmd/entity-graph` | 8-K Item 5.07 → director graph, governance signals, RSI feedback loop |
| `cmd/eps-processor` | Press-release bodies → extracted EPS results (oracle cases + articles) |
| `cmd/eps-reconciler` | Reconciles EPS oracle cases against filed 8-K numbers (confirms/contradicts) |
| `cmd/jon-agent` | Cross-signal divergence analysis, surfaces options setups (:8084) |
| `cmd/projector` | Maintains the MySQL/SQLite read-model schema from raw events |

**Serving** — expose the data
| Process | Purpose |
|---------|---------|
| `cmd/newssite` | Public news reader (:8082) — front page, ticker pages with own-hosted charts, wire, movers, commentary, RSS, Ask Emily |
| `cmd/signalapi` | HTTP signal query API (:9091), OpenAPI 3.1 spec + Swagger playground |
| `cmd/dashboard` | SSE event dashboard (:8080) |
| `cmd/feedserver` | TCP framed feed (:8083) |
| `cmd/broker` | Tenant-aware proxy with hot-reload registry |
| `cmd/emily-agent` | LLM ops agent + signal analyst (:8080/:8086 depending on deployment) |

**RSI / operations**
| Process | Purpose |
|---------|---------|
| `cmd/observation-watcher` | Invokes Claude Code when a new observation is published; `--continue` for the full AGI loop |
| `cmd/earnings-alert` | Weekly email digest of upcoming earnings (Mailgun/SMTP/null backend) |
| `cmd/bob-agent` | MySQL schema admin agent (destructive ops require explicit confirm) |
| `cmd/graph-seeder` | Seeds the SQLite signalapi read-model fallback from entity-graph output |

**One-off / migration tools** (not part of steady-state operation)
`cmd/backfill-signal-dates`, `cmd/norn-entitygraph-migrate`, `cmd/norn-eps-migrate`,
`cmd/stub-backfill`, `cmd/eventstore-demo`, `cmd/fatstream-tail`, `cmd/replay`.

Data: `./var/<process>/`. Logs: `./var/logs/<process>.log`. Supervision: user-level systemd units
in `ops/systemd/*.service` (+ `.timer` for daily jobs like `movers-watcher`) — see each unit file's
header comment for deploy steps; no `sudo` required for any of them.

---

## Patterns

The reusable idioms this codebase has converged on. Building a new collector or consumer almost
always means reaching for one of these rather than inventing a new shape.

- **Event store, not a database.** `eventstore.FileStore` — file-backed, append-only NDJSON,
  UTC-date-partitioned, monotonic sequence numbers. Every process resumes from its own cursor
  file after a restart. Never mutate or reorder a written record.
- **`Scan`, not `ReadFrom`, for anything that walks history.** `eventstore.Scan(ctx, fromSeq, fn)`
  streams each journal file exactly once, line by line, without materializing a whole file —
  `ReadFrom`'s paged reads are fine for small targeted fetches but pathological (quadratic) over
  a large store. See `docs/northstar/replay-fragility.md` for what happens when a process uses
  the wrong one at scale.
- **Build/Tail per-process indexes.** `signalindex`, `docindex`, and siblings all follow the same
  shape: `Build(ctx, store, idx, fromSeq, logger)` does the initial scan, `Tail(ctx, store, idx,
  interval, logger)` polls for new records after. Every in-memory read model in this repo looks
  like this.
- **SQLite checkpoints for anything whose Build is no longer cheap.** `internal/indexcheckpoint`
  persists an index's state to a small local SQLite file — self-heals on a missing/corrupt/
  version-mismatched file (a checkpoint is a disposable cache, never a source of truth; deleting
  one is always safe). Watermark = the event store's *true current end*
  (`store.LatestSequence()`), not the highest sequence among only *matching* records — the latter
  causes redundant rescans on every warm start. Used by `signalapi`, `newssite`, and
  `entity-graph`'s filing index.
- **`httpretry.Do[T]` for anything hitting a flaky/rate-limited upstream.** Exponential
  backoff+jitter, retryable-status classification (429/403/5xx) built in. Every external HTTP
  collector (`market-data-watcher`, `movers-watcher`, `secwatch`) uses it instead of hand-rolling
  retry logic.
- **Gate on real-world state before publishing, not just on a cron tick.** `internal/marketcal`
  answers "is the market actually open today" from real NYSE holiday rules (not a hardcoded
  per-year table). `movers-watcher` checks this itself even though its own systemd timer already
  has a schedule — a timer misfire or manual run on a holiday must still no-op correctly.
- **The editorial ticker-linking standard.** `internal/tickerlink.FormatRef` — "Company Name
  (EXCHANGE:TICKER)" with the ticker as a real, *absolute* link to our own ticker page (absolute
  so the link survives syndication/copy-paste elsewhere). Every content generator that mentions a
  ticker uses this instead of ad-hoc string formatting.
- **Compiled binaries under systemd, not `go run` in a terminal.** `ops/systemd/fatbaby-*.service`
  — `Restart=on-failure`, `MemoryMax` set to observed usage + headroom, logs to
  `var/logs/<process>.log`. User-level units (`systemctl --user`), no `sudo` needed. A process
  left running as `go run` in a background shell is a known gap, not a design choice.
- **Auth on anything that writes, not just reads.** Most GET endpoints here are intentionally
  public (that's the point of a news site). Every POST/write path needs either a static bearer
  token (constant-time comparison) or an IDUNA-issued JWT — an unauthenticated write endpoint is
  always a bug, not a temporary convenience, however it got that way.

---

## Configuration

### `config/watchlist.json`

50 companies tracked: megacap tech (AAPL, MSFT, GOOG/GOOGL, META, NVDA, AMZN, TSLA), financials
(SCHW, GS, MS, JPM, BAC, WFC, BLK, STT, C, IBKR, COIN, SOFI, MSTR), and a spread across
healthcare, energy, industrials, and retail. Every ticker here is what `secwatch`/`prwatch` poll
for, what `market-data-watcher` charts, and what "tracked" means on the daily movers article —
expanding this list is the single biggest lever on how much of the site has real context instead
of numbers-only coverage.

### `config/entity-graph-rules.json`

Hot-reloadable thresholds. Key fields:

```json
{
  "friction_threshold": 0.85,
  "nomination_rejection_threshold": 0.50,
  "entrenchment_min_for": 0.80,
  "broker_nonvote_anomaly_threshold": 0.12,
  "activist_risk_window_days": 365,
  "family_name_keywords": ["schwab", "walton", "mars", "buffett", "fidelity", "johnson"]
}
```

---

## Runtime data layout

```
var/
├── secwatch/           filing_discovered + source_document_persisted events
├── entity-graph/
│   ├── nodes.ndjson    PersonNode records (compacted on startup)
│   ├── edges.ndjson    board co-member edges
│   ├── signals.ndjson  all governance signals
│   └── auditors.ndjson most recent auditor per ticker
├── emily-observations/
│   ├── latest.json     current observation (triggers observation-watcher)
│   └── <timestamp>.json archived observations
└── logs/
```

---

## Testing

```bash
go test ./...
```

---

## Northstar roadmap

| Phase | Status | Description |
|-------|--------|-------------|
| 1 — Core extraction | ✅ | 8-K parser, entity graph, 13 signal types, 40+ tests |
| 2 — Recursive loop | ✅ | Observation-watcher, Claude refinement, hot-reload rules, severity gate, Emily Prime integration |
| 3 — Multi-company | ✅ | 50-ticker watchlist; director centrality; `director_link` fires on shared directors; IDUNA JWT auth |
| 4 — Enrichment | 🔲 | FEC donor data, FARA lobbying, SEC comment letters |

---

## Further reading

- [Northstar: 8-K Intelligence Engine](docs/northstar/northstar.md)
- [Distributed event intelligence architecture](docs/architecture-distributed-event-intelligence.md)
- [News site end-to-end runbook](docs/news-site-e2e-runbook.md)
