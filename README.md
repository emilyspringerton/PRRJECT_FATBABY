# prrject-fatbaby

A Go-based financial signal intelligence pipeline. It watches SEC EDGAR filings and PR Newswire press releases, extracts structured governance signals from the content, and exposes them through a streaming TCP feed, an SSE dashboard, and an LLM-powered operations agent (Emily) that can both monitor the system and answer questions about what the signals mean.

The centrepiece is a **recursive self-improving entity graph** that turns 8-K annual meeting filings into director-level governance intelligence — friction scores, activist risk composites, entrenchment patterns, and cross-company board relationships — and feeds anomalies back to Claude Code for automatic rule refinement.

---

## Architecture

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

| Process | Purpose |
|---------|---------|
| `cmd/secwatch` | Polls SEC EDGAR → `filing_discovered` events |
| `cmd/prwatch` | Polls PR Newswire → discovery events |
| `cmd/prwatch-body` | Fetches press release bodies |
| `cmd/processor` | Filings → structured signals |
| `cmd/entity-graph` | 8-K Item 5.07 → director graph + governance signals |
| `cmd/observation-watcher` | Triggers Claude when entity-graph publishes an observation |
| `cmd/emily-agent` | LLM ops agent + signal analyst (:8080) |
| `cmd/dashboard` | SSE event dashboard (:8080) |
| `cmd/newssite` | Filing reader (:8082) |
| `cmd/feedserver` | TCP framed feed (:8083) |
| `cmd/signalapi` | HTTP signal query API (:8084) |
| `cmd/broker` | Tenant-aware proxy with hot-reload registry |

Data: `./var/<process>/`. Logs: `./var/logs/<process>.log`.

---

## Configuration

### `config/watchlist.json`

25 companies pre-configured: SCHW, GS, MS, JPM, BAC, WFC, BLK, STT, C, IBKR, BRK.A/B, AAPL, MSFT, NVDA, BEN, PLTR, MSTR, LLY, and others. 8-K filings with Item 5.07 feed the entity-graph pipeline.

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
| 1 — Core extraction | ✅ | 8-K parser, entity graph, 12 signal types, 30+ tests |
| 2 — Recursive loop | ✅ | Observation-watcher, Claude refinement, hot-reload rules, severity gate |
| 3 — Multi-company | 🔄 | 25-company watchlist; director centrality; `director_link` fires on shared directors |
| 4 — Enrichment | 🔲 | FEC donor data, FARA lobbying, SEC comment letters |

---

## Further reading

- [Northstar: 8-K Intelligence Engine](docs/northstar/northstar.md)
- [Distributed event intelligence architecture](docs/architecture-distributed-event-intelligence.md)
- [News site end-to-end runbook](docs/news-site-e2e-runbook.md)
