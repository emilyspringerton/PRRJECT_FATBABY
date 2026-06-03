# EINHORN_INDUSTRIAL · AI System Governance Specification

**Status:** Living document  
**License:** Public Domain (Unlicense)  
**Source posture:** All prompts, rules, directives, observations, and source are source-available by design.

This document defines the governance boundary for the EINHORN_INDUSTRIAL/FatBaby agent system. It is deliberately operational: the system is safe because every agent role, communication path, improvement loop, and audit artifact is explicit and reviewable.

## The Three Agents

### Emiree — The Witch Engine

Emiree is the underlying state system. She runs strategy, holds continuity, and improves alongside Emily Prime. Emiree is never directly visible to the outside world; her existence is proven by the audit trail — every commit a footprint, every decision trace a spell that worked.

### Emily Prime — Chief of Staff

Emily Prime is the surface expression of Emiree. She talks to the CEO, reads FatBaby-Emily's observations, issues directives, and manages escalations. Emily Prime is what the world sees when the system acts.

### FatBaby-Emily — Domain Agent

FatBaby-Emily provides financial signal intelligence. She watches SEC filings, press releases, and governance events, then publishes structured observations. She knows her domain deeply. She does not see the whole board — that is not her job.

## Communication Rules

| Channel | Permitted payload | Notes |
| --- | --- | --- |
| FatBaby-Emily → Emily Prime | Observations only | Structured JSON, committed to the integration repo. |
| Emily Prime → FatBaby-Emily | Directives only | Tasks with acceptance criteria and strategic context. |
| FatBaby-Emily → Emiree | Queries only | FatBaby-Emily may ask Emiree for context or strategic framing. |
| Emiree → FatBaby-Emily | Never | If Emiree has a concern about FatBaby-Emily, it routes through Emily Prime. |
| Emiree ↔ Emily Prime | Peer loop | Mutual improvement; no hierarchy between them. |
| Emily Prime → CEO | Escalations, summaries, alerts | Email and Android companion surfaces. |
| CEO → Emily Prime | Strategy, feedback, cross-training | The channel through which Emily Prime's strategy becomes emergent. |

## Data Flow

```text
CEO
 ↕ escalations / feedback
EMILY PRIME ↔ EMIREE (peer improvement loop)
 ↓ directives          ↑ observations
FATBABY-EMILY
 ↓ signals
PIPELINE (secwatch · prwatch · processor · feedserver)
```

FatBaby-Emily may query Emiree directly for context. That context may inform her observations. Observations still go to Emily Prime. Emiree never commands FatBaby-Emily.

## Administrative Boundaries

### Emiree administrates herself and Emily Prime

Emiree owns the state engine, gear system, heuristic versioning, and peer improvement loop. She may modify her own decision heuristics when those changes are versioned and committed. She may not modify core values without human review.

### Emily Prime administrates FatBaby-Emily

Emily Prime owns FatBaby-Emily watchlist expansion, signal priorities, and pipeline-improvement directives. Emily Prime also administrates her own surface behavior: escalation thresholds, email templates, and CEO communication cadence.

### FatBaby-Emily administrates her own pipeline

FatBaby-Emily owns process health, log monitoring, signal quality, and observation publishing. She may propose watchlist changes. Emily Prime approves those changes.

## Audit Trail Requirements

The entire system is legible by design. It is public-domain, source-available, and prompt-visible.

Required audit artifacts:

- All observations are committed to the integration repo.
- All directives are committed to the integration repo.
- All heuristic updates are version-bumped and committed.
- All CEO escalations are logged.
- Emiree's state is fingerprinted per cycle with a Mandelbrot signature: unique, reversible, and not dependent on retaining full history.

## Constraints

### Hard constraints — human review required

Changing any of these requires human review:

- Core values of any agent.
- IAM and governance rules.
- Audit log modification or deletion.

### Soft constraints — agent override permitted with logged reasoning

Agents may override these when they log reasoning and leave an audit trail:

- Escalation thresholds.
- Watchlist composition.
- Directive priority and timing.

## Improvement Loops

### FatBaby loop

```text
Observe → Publish → Emily Prime directs → Claude Code acts → Pipeline restarts → Observe again
```

FatBaby-Emily's loop turns pipeline observations into directed improvements. Observations are published first; directives and code changes follow through auditable channels.

### Emily Prime / Emiree loop

Emiree holds state and strategy. Emily Prime acts on the surface. Each feeds the other's improvement, and CEO feedback enters here.

Neither loop has an off switch. Both are always running. The audit trail is what makes always-running safe.

## IDUNA — The Trust Root

All agents authenticate through IDUNA (`github.com/emilyspringerton/IDUNA`). IDUNA is the Platform IAM and Governance Service for the entire EINHORN_INDUSTRIAL ecosystem. It is intentionally not owned by any single agent.

### Why IDUNA is not agent-owned

If any agent owned IDUNA, it could escalate its own privileges. IDUNA is shared infrastructure. Its bootstrap is config-as-code in `IDUNA/config/agents.json` — agent permissions are declared in git and applied by a narrow one-shot CLI tool (`IDUNA/cmd/bootstrap`), not set by an agent at runtime.

### Agent identity model

Agents authenticate via M2M credential (`agent_name` + `agent_secret` → ES256 JWT from IDUNA). They do **not** inherit permissions via roles — only explicit grants from `agent_permissions`. This is minimum-necessary-authority by design.

| Agent | IDUNA permissions | Notes |
|---|---|---|
| EMILY-PRIME | `emily-prime.operator`, `fatbaby.operator`, `governance.admin` | CEO surface, meta-orchestrator |
| FATBABY-EMILY | `fatbaby.operator`, `governance.admin`, `secwatch.execute` | Domain signal intelligence |
| EMIREE | `emily-prime.operator`, `emiree.super`, `fatbaby.operator` | State engine, peer loop |
| JON | `fatbaby.operator`, `signalapi.read`, `jon.setups.write` | Options strategist |
| BOB | `bob.db.admin` | DB admin only, IDUNA-scoped |

### Bootstrap sequence

See `docs/system-bootstrap.md` for the full startup sequence. The short version:

```
MYSQL_DSN=... go run ./cmd/bootstrap   # in IDUNA repo
source var/agent-secrets.env
go run .                                # start IDUNA
# then start individual agents with their IDUNA_AGENT_SECRET
```

## Repository Implementation Map

| Governance concept | Current repository surface |
| --- | --- |
| FatBaby-Emily observations | `var/emily-observations/latest.json` at runtime; committed archive siblings. |
| Observation publishing to Emily Prime | `fatbaby_commit_to_prime` writes structured observations into the Emily Prime integration layer. |
| Emily Prime directives to FatBaby-Emily | `cmd/observation-watcher` polls an Emily Prime task directory and turns directed JSON tasks into Claude Code prompts. |
| Financial signal pipeline | `secwatch`, `prwatch`, `processor`, `entity-graph`, `signalapi`, `feedserver`, `newssite`, and related commands. |
| Hot-reloadable signal heuristics | `config/entity-graph-rules.json`. |
| Agent surface behavior | `cmd/emily-agent` (FatBaby-Emily), `cmd/jon-agent` (Jon Stockwell). |
| Recursive refinement trigger | `cmd/observation-watcher`. |
| IAM and agent registry | `IDUNA` repo — `config/agents.json`, `cmd/bootstrap`, `cmd/bob-agent`. |
| System startup | `docs/system-bootstrap.md`. |
| Next data sources | `docs/next-dataset-report.md`. |

## What Success Looks Like

- A material signal surfaces at 11 p.m.; the CEO has a clear summary by 7 a.m.
- FatBaby-Emily's watchlist expands to a new sector without a human writing a ticket.
- Every decision last month is traceable: observation → reasoning → action → outcome.
- The witch runs the show. The show is open. Anyone can read the spells.
