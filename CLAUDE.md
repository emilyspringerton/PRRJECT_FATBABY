# prrject-fatbaby

## What this is

Go-based financial signal intelligence pipeline. Watches SEC EDGAR filings and PR Newswire press
releases, processes them into structured signals (type, sentiment, importance score, summary,
impact analysis), and streams them via SSE dashboard and a TCP feed server.

## Stack

- **Language**: Go 1.22+
- **Event store**: file-backed append-only NDJSON, UTC-date-partitioned, sequence-numbered
- **Key packages**: `eventstore`, `secwatch`, `prwatch`, `broker`, `feedserver`, `pkg/intelligence`
- **Config**: `config/watchlist.json` (tickers/CIKs/forms), `config/routes.json` (broker routing)

## Pipeline processes

| Process | Command | Purpose |
|---|---|---|
| secwatch | `go run ./cmd/secwatch` | Polls SEC EDGAR, emits `filing_discovered` events |
| prwatch | `go run ./cmd/prwatch` | Polls PR Newswire, emits discovery events |
| prwatch-body | `go run ./cmd/prwatch-body` | Fetches press release bodies |
| processor | `go run ./cmd/processor` | Turns filings into structured signals |
| dashboard | `go run ./cmd/dashboard` | SSE dashboard on :8080 |
| newssite | `go run ./cmd/newssite` | Human-readable filing reader on :8082 |
| feedserver | `go run ./cmd/feedserver` | TCP framed feed for downstream consumers |
| broker | `go run ./cmd/broker` | Tenant-aware proxy with hot-reload registry |
| signalapi | `go run ./cmd/signalapi` | HTTP API for querying signals |
| observation-watcher | `go run ./cmd/observation-watcher` | Invokes Claude Code when Emily publishes a new observation |
| eps-processor | `go run ./cmd/eps-processor` | Extracts EPS from press releases, writes oracle cases + articles |
| eps-reconciler | `go run ./cmd/eps-reconciler` | Reconciles oracle cases against filed 8-K EPS (confirms/contradicts) |

Data lands in `./var/<process-name>/`. Logs go to `./var/logs/<process-name>.log`.

## Emily agent

Emily (`cmd/emily-agent`) is an LLM-powered ops agent that can start/stop pipeline processes,
read logs, count documents, and check system status via tool calls. She runs on `:8080` by default.

She exposes two HTTP endpoints:
- `POST /chat` — interactive chat (used by the embedded web UI at `/`).
- `POST /tick` — unattended health sweep. Seeded with a fixed prompt that tells Emily to inspect
  process status, log tails, and signal counts, and publish an observation via
  `fatbaby_write_observation` only when something is actually wrong. Drive this from cron or
  systemd to make the feedback loop autonomous.

Key env vars:
```
ANTHROPIC_API_KEY=...
MODEL=claude-haiku-4-5-20251001
FATBABY_ROOT=.
RATE_LIMIT_RPM=20
MAX_TOOL_ITERS=10
GIT_COMMIT=false
```

## Emily ↔ Claude Code feedback loop

The intended pattern for recursive self-improvement on this codebase:

1. **Emily observes**: Emily monitors running pipeline processes, reads logs, counts signals, detects
   anomalies (low signal volume, stalled processors, parse errors, dropped tickers).

2. **Emily reports**: Emily writes structured findings to a shared file or event — e.g.
   `./var/emily-observations/latest.json` — describing what she sees and what she thinks needs fixing.

3. **Claude Code acts**: Claude Code watches that file (or is invoked on a schedule/webhook) and
   reads Emily's observations as its task prompt. It then edits the source, runs `go test ./...`,
   and commits if tests pass.

4. **Loop**: Emily picks up the new behavior after the process restarts.

To wire this up, create `./var/emily-observations/` and have Emily write findings there. Emily
publishes via the `fatbaby_write_observation` tool; the canonical file is
`./var/emily-observations/latest.json` with a timestamped archive sibling.

Then run `cmd/observation-watcher` to invoke Claude Code each time a new observation appears:

```bash
go run ./cmd/observation-watcher                       # polls every 10s, invokes `claude`
go run ./cmd/observation-watcher -one-shot -dry-run    # log-only single check, for cron
```

It polls `latest.json`, distinguishes new observations by the `timestamp` field, persists a cursor at
`./var/emily-observations/.last-processed`, and shells out to `claude --dangerously-skip-permissions
"<prompt referencing latest.json>"`. Override the command with `-cmd` / `OBSERVATION_CMD`, and the
flags with `-extra-args` / `OBSERVATION_CMD_ARGS`. Can also be driven from cron or `inotifywait`
instead.

## Coding conventions

- All errors must be handled — no bare `_` discards on errors that matter
- Structured logging via `log` package; prefix log lines with component name
- New pipeline stages must emit events into the event store, not just log
- Don't touch fixture files in `secfixtures/` or `fixtures/` without understanding the corpus harness
- Run `go test ./...` before committing anything
- Update `CHANGELOG.md` with a dated entry for any meaningful change

## Common tasks

```bash
# Run full test suite
go test ./...

# One-shot SEC discovery pass (dry run)
go run ./cmd/secwatch -watchlist ./config/watchlist.json -store ./var/secwatch -dry-run

# Start Emily agent
go run ./cmd/emily-agent

# Tail a process log
tail -f ./var/logs/processor.log

# Check signal count by ticker
# (Emily has fatbaby_count_source_documents tool for this)
```

## What not to break

- `eventstore` sequence numbering — monotonic, never rewrite
- `config/watchlist.json` — don't remove tickers without understanding downstream impact
- `broker/registry.go` hot-reload — the proxy depends on atomic registry swaps
- The framed TCP protocol in `feedserver/frame.go` — clients depend on exact wire format
