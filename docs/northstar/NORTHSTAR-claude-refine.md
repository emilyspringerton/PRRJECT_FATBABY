# NORTHSTAR-claude-refine: Entity Graph Self-Improvement Prompt Template

This document describes the automated prompt that `observation-watcher` builds and sends to
Claude Code each time entity-graph publishes a new observation. The prompt is constructed
programmatically by `cmd/observation-watcher/main.go:buildEntityGraphPrompt` — this file is the
human-readable specification and reference for that function.

## When this fires

`observation-watcher` polls `var/emily-observations/latest.json` every 10 seconds. When the
content hash changes (excluding timestamp), it invokes:

```
claude --dangerously-skip-permissions "<prompt>"
```

The prompt is generated from the observation JSON + the current `config/entity-graph-rules.json`.

## Prompt structure (generated)

```
You are acting as the NORTHSTAR recursive self-improvement engine for the entity-graph
intelligence pipeline. An observation has been published from the entity-graph process.
Your job is to refine the signal rules or parser based on what you see.

## Observation (var/emily-observations/latest.json)
Source:            entity-graph
Status:            needs_attention | ok | error
Subject:           Entity graph run: N filings, M directors, K signals
Filings processed: N
Directors found:   M
Signals generated: K
Signals by type:
  director_friction: N
  high_trust_director: N
  ...
Gaps detected:
  - No entrenchment signals detected — proxy vote sections may not be parsed yet
  - No family control signals — family_name_keywords in rules may need expansion

## Request from entity-graph
<RequestForClaude field from observation JSON>

## Current Signal Rules (config/entity-graph-rules.json)
```json
{ ... current rules ... }
```

## Your task
1. Analyze the gaps and parse errors above.
2. If signal thresholds need adjustment, edit `config/entity-graph-rules.json` directly
   — do NOT touch Go source for threshold changes.
3. If parse errors are structural (regex misses, form variants), edit
   `internal/entitygraph/parser.go` and add a test to `internal/entitygraph/parser_test.go`.
4. Run `go test ./...` to verify all changes.
5. Commit passing changes with a descriptive message explaining the refinement.
6. Document the change in CHANGELOG.md with today's date.
```

## What Claude should edit (in priority order)

| Scenario | Target file |
|---|---|
| Threshold too high/low (friction, high-trust, entrenchment) | `config/entity-graph-rules.json` |
| Family name keyword missing | `config/entity-graph-rules.json` |
| Regex misses a name format or vote table variant | `internal/entitygraph/parser.go` |
| New signal type needed | `internal/entitygraph/signals.go` + rules + tests |

## Feedback loop cadence

```
entity-graph process runs (every 30s, polling secwatch event store)
  ↓ publishes observation to var/emily-observations/latest.json
observation-watcher detects content change
  ↓ builds prompt (observation + current rules)
  ↓ invokes: claude --dangerously-skip-permissions "<prompt>"
Claude edits entity-graph-rules.json (or parser.go)
  ↓ runs go test ./...
  ↓ commits passing changes
entity-graph reloads rules on next batch (no restart needed)
  ↓ publishes new observation reflecting improved signal coverage
loop continues
```

## Deduplication

The watcher hashes observation content excluding `timestamp`. Emily can safely re-publish the
same observation on every health tick without triggering redundant Claude invocations.
Claude is only re-invoked when `gaps`, `signals_by_type`, `parse_errors`, `status`, or
`request_for_claude` actually change.

## Deep-dive weekly prompt

For the comprehensive weekly self-improvement prompt (pattern discovery, retrospective accuracy
measurement, data source prioritization), see `docs/northstar/claude_refinement_prompt.mdwq`.
That template is for human-driven or scheduled weekly runs, not the automated per-observation loop.
