# QUICK START: Running the FATBABY Intelligence Pipeline
## Operational runbook for the live system

**System status**: All four layers operational as of May 2026.
This document covers how to run, operate, and extend the live system.

---

## Prerequisites

```bash
export ANTHROPIC_API_KEY="sk-ant-..."   # required for Emily agent and observation-watcher
go version                               # Go 1.22+ required
ls config/watchlist.json                 # should exist
ls config/entity-graph-rules.json        # should exist
```

---

## Starting the full pipeline

Each process writes logs to `var/logs/<process-name>.log` and data to `var/<process-name>/`.

### Core data ingestion

```bash
# SEC EDGAR filing poller — emits source_document_persisted events
go run ./cmd/secwatch -watchlist ./config/watchlist.json -store ./var/secwatch

# Entity graph + signal scorer — reads 8-K filings, writes nodes/edges/signals.ndjson
# Also publishes observations to var/emily-observations/latest.json
go run ./cmd/entity-graph \
  -store ./var/secwatch \
  -graph-dir ./var/entity-graph \
  -obs-dir ./var/emily-observations \
  -rules ./config/entity-graph-rules.json
```

### The newssite (`:8082`)

```bash
go run ./cmd/newssite \
  -store ./var/secwatch \
  -graph-dir ./var/entity-graph \
  -addr :8082
```

Open `http://localhost:8082`. The front page leads with the highest-severity live governance signal.

### Emily (`:8080`) + the recursive refinement loop

```bash
# Start Emily — LLM ops agent with process management tools
go run ./cmd/emily-agent

# Start the observation-watcher — polls var/emily-observations/latest.json every 10s
# and invokes Claude Code when a new observation arrives
go run ./cmd/observation-watcher
```

Emily's web UI is at `http://localhost:8080`. Her `/tick` endpoint (POST) runs an unattended health sweep.

---

## Running entity-graph manually (one shot)

Use this to process a fresh batch and inspect what signals it produced:

```bash
go run ./cmd/entity-graph \
  -store ./var/secwatch \
  -graph-dir ./var/entity-graph \
  -obs-dir ./var/emily-observations \
  -rules ./config/entity-graph-rules.json

# Check the observation it published
cat var/emily-observations/latest.json | jq '{severity, summary, signals_by_type}'

# Check the signals it wrote
cat var/entity-graph/signals.ndjson | tail -20 | jq '{type, ticker, severity, filing_date}'
```

---

## The recursive refinement loop

Emily publishes structured observations. The observation-watcher turns them into Claude prompts. Claude edits the rules config or parser.

### Trigger a refinement manually

```bash
# Ask Emily to do a health sweep and write an observation (via HTTP)
curl -s -X POST http://localhost:8080/tick | jq .

# If observation-watcher is running, it will auto-invoke Claude Code
# within ~10 seconds of the observation being written.

# To watch it happen:
tail -f var/logs/observation-watcher.log
```

### What Claude is allowed to edit

See `docs/northstar/NORTHSTAR-claude-refine.md` for the full specification of what the
automated prompt contains and which files Claude should target.

| Scenario | File Claude edits |
|---|---|
| Threshold too high/low | `config/entity-graph-rules.json` |
| Family keyword missing | `config/entity-graph-rules.json` |
| Regex misses a name format | `internal/entitygraph/parser.go` |
| New signal type needed | `internal/entitygraph/signals.go` + rules + tests |

After each Claude invocation, entity-graph reloads `entity-graph-rules.json` on its next batch
run (no restart required). Go source changes take effect on the next `go run`.

---

## Where data lives

```
var/secwatch/               event store (source_document_persisted records, NDJSON partitioned by date)
var/entity-graph/
  nodes.ndjson              person nodes — one record per canonical director, upserted across filings
  edges.ndjson              board co-membership edges between directors
  signals.ndjson            scored governance signals (13 types)
  auditors.ndjson           auditor tracking per ticker (with change detection)
  accuracy.ndjson           activist_risk retrospective accuracy vs Schedule 13D filings
var/emily-observations/
  latest.json               canonical observation file (observation-watcher polls this)
  *.json                    timestamped archive copies
var/logs/
  secwatch.log
  entity-graph.log
  newssite.log
  emily-agent.log
  observation-watcher.log
```

---

## Inspecting signals

```bash
# All signals for a ticker
cat var/entity-graph/signals.ndjson | jq 'select(.ticker == "SCHW")'

# Just high/critical signals
cat var/entity-graph/signals.ndjson | jq 'select(.severity == "high" or .severity == "critical")'

# Signal counts by type
cat var/entity-graph/signals.ndjson | jq -r '.type' | sort | uniq -c | sort -rn

# Director nodes
cat var/entity-graph/nodes.ndjson | jq '{name, canonical_id, filings: (.filings | length)}'
```

---

## Tuning signal rules

Rules live in `config/entity-graph-rules.json`. Entity-graph reloads this file at each batch
start — no restart needed after editing.

Key thresholds:

| Field | Meaning | Default |
|---|---|---|
| `friction_threshold` | Approval % below which `director_friction` fires | 0.90 |
| `high_trust_min_approval` | Approval % above which `high_trust_director` fires | 0.95 |
| `nomination_rejection_threshold` | Approval % below which `nomination_rejection` fires | 0.50 |
| `comp_exec_alert_threshold` | Against-vote % triggering `compensation_concern` | 0.25 |
| `abstention_spike_threshold` | Abstention % triggering `abstention_spike` | 0.10 |
| `family_name_keywords` | Names triggering `family_control` signal | see file |

---

## Troubleshooting

### "No signals generated"

```bash
# Check if entity-graph processed any 8-K filings
grep "processing seq=" var/logs/entity-graph.log | tail -20

# Check for skip_no_507 (8-K without a vote section — normal for non-proxy 8-Ks)
grep "skip_no_507" var/logs/entity-graph.log | wc -l

# Check for parse errors
grep "parse_item507" var/logs/entity-graph.log
```

### "Observation-watcher not firing"

```bash
# Verify the watcher is running
grep "new observation" var/logs/observation-watcher.log | tail -5

# Check the last-processed cursor vs latest.json timestamp
cat var/emily-observations/.last-processed
cat var/emily-observations/latest.json | jq .timestamp
```

### "Front page shows historical filings as today's news"

This was a known bug (fixed 2026-05-31): entities with `filing_date` > 90 days before today were
appearing on the front page. The fix:
- Signals now carry a `filing_date` field populated from SEC filing metadata (not pipeline processing time)
- Front page filters out signals/documents whose `filing_date` is >90 days old
- Historical articles still appear on ticker pages, with an "ARCHIVE" kicker

---

## Running tests

```bash
go test ./...
```

All pipeline packages have unit tests. The entitygraph and newssite packages have the most coverage.

---

*The data is already here. The loop is already running. Start with `http://localhost:8082`.*
