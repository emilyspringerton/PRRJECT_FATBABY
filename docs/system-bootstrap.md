# EINHORN_INDUSTRIAL — System Bootstrap Architecture

**Date:** 2026-06-03  
**Status:** Implemented (IDUNA bootstrap) / Planned (full system startup)

---

## The Problem

The EINHORN_INDUSTRIAL system has a bootstrap dependency chain:

```
MySQL must exist
  → IDUNA needs MySQL to start
  → Bob needs IDUNA to authenticate
  → Other agents need IDUNA to authenticate
  → IDUNA permissions need to be set for agents to work
  → But no one wanted to set permissions manually in the admin UI
```

IDUNA is intentionally not owned by any single agent — it is shared infrastructure. This means no one agent can "set up" IDUNA autonomously. The bootstrap must be a narrow, purpose-built tool, not an agent with broad system authority.

---

## The Solution: cmd/bootstrap

`IDUNA/cmd/bootstrap` is a narrow, one-shot CLI tool — not an agent, not an LLM, no HTTP endpoints. Its authority is scoped to exactly three actions:

1. Run pending DB migrations
2. Seed agent permissions from `IDUNA/config/agents.json`
3. Generate agent API key secrets → write to `var/agent-secrets.env`

It exits. It does not stay running. It has no ability to affect application logic, push code, or control any agent.

**Config as code:** Agent identities, types, and minimum-necessary permissions are declared in `IDUNA/config/agents.json`. This file is committed to git. Changing an agent's authority means editing the file and re-running bootstrap — no admin UI, no manual SQL.

---

## Full System Startup Sequence

```
Step 1: MySQL
  Ensure MySQL 8.0+ is accessible.
  Set MYSQL_DSN="user:pass@tcp(host:3306)/iduna?parseTime=true"

Step 2: Bootstrap (IDUNA repo, idempotent)
  cd IDUNA
  go run ./cmd/bootstrap
  → runs migrations
  → seeds agent permissions from config/agents.json
  → generates secrets to var/agent-secrets.env

Step 3: Source secrets
  source var/agent-secrets.env
  # provides IDUNA_SECRET_EMILY_PRIME, IDUNA_SECRET_FATBABY_EMILY,
  # IDUNA_SECRET_EMIREE, IDUNA_SECRET_JON, IDUNA_SECRET_BOB

Step 4: Start IDUNA
  cd IDUNA && go run .
  # listens :8080

Step 5: Bob comes online (DB admin)
  MYSQL_DSN="..." IDUNA_AGENT_SECRET="${IDUNA_SECRET_BOB}" \
  go run ./cmd/bob-agent   # :8083

Step 6: FatBaby pipeline
  cd PRRJECT_FATBABY
  IDUNA_BASE_URL="http://localhost:8080" \
  IDUNA_AGENT_NAME="FATBABY-EMILY" \
  IDUNA_AGENT_SECRET="${IDUNA_SECRET_FATBABY_EMILY}" \
  go run ./cmd/emily-agent   # :8080 (fatbaby port)

Step 7: Jon Stockwell
  IDUNA_BASE_URL="http://localhost:8080" \
  IDUNA_AGENT_NAME="JON" \
  IDUNA_AGENT_SECRET="${IDUNA_SECRET_JON}" \
  go run ./cmd/jon-agent   # :8084

Step 8: Emily Prime and Emiree
  # Emily Prime and Emiree agents live in the EMILY repo.
  # They authenticate with their respective secrets.
  IDUNA_AGENT_NAME="EMILY-PRIME" IDUNA_AGENT_SECRET="${IDUNA_SECRET_EMILY_PRIME}" ...
  IDUNA_AGENT_NAME="EMIREE" IDUNA_AGENT_SECRET="${IDUNA_SECRET_EMIREE}" ...
```

---

## Agent Authority Model

Agents in IDUNA do NOT inherit permissions via roles. They receive explicit grants only, from `agent_permissions`. This is the minimum-necessary-authority design.

| Agent | Type | Key permissions | Notes |
|---|---|---|---|
| EMILY-PRIME | llm_agent | `fatbaby.operator`, `emily-prime.operator`, `governance.admin`, `apples.write` | CEO-facing surface, meta-orchestrator |
| FATBABY-EMILY | llm_agent | `fatbaby.operator`, `governance.admin`, `secwatch.execute`, `apples.write` | Domain signal intelligence |
| EMIREE | llm_agent | `fatbaby.operator`, `emily-prime.operator`, `emiree.super`, `governance.admin` | State engine, peer loop with Emily Prime |
| JON | llm_agent | `fatbaby.operator`, `signalapi.read`, `jon.setups.write` | Options strategist, read-only pipeline access |
| BOB | db_agent | `bob.db.admin`, `iduna.me.read` | DB admin only, no system control |

**Why Bob cannot set up agents:** Bob's authority is bounded to `bob.db.admin`. He can inspect schema and run migrations, but he cannot grant permissions, create agents, or rotate credentials. This is intentional — the bootstrap tool handles provisioning at startup, not the DB agent.

**Why IDUNA is not owned by an agent:** IDUNA's role is to be the trust authority for all agents. If any single agent owned IDUNA, it could escalate its own privileges. The bootstrap tool + config-as-code pattern removes that possibility — permissions are declared in git, not set by an agent at runtime.

---

## Emiree's Role in the System

Emiree is the state engine and strategic layer. She is peer with Emily Prime, not above or below her. She:
- Holds continuity and strategy across sessions
- Runs the peer improvement loop with Emily Prime
- Can issue context to FatBaby-Emily (queries only, never commands)
- Never commands FatBaby directly (routes through Emily Prime if needed)

Emiree's IDUNA permissions include `emiree.super` — a marker that downstream services can check to identify the Emiree identity specifically, without granting her authority over other agents' infrastructure.

---

## Adding a New Agent

1. Write a migration in `IDUNA/migrations/truestore/` seeding the agent row (deterministic UUID, `NULL` api_key_hash)
2. Add the agent entry to `IDUNA/config/agents.json` with permissions
3. Re-run `go run ./cmd/bootstrap` — generates a secret, writes to `var/agent-secrets.env`
4. Start the new agent process with `IDUNA_AGENT_NAME=<NAME> IDUNA_AGENT_SECRET=${IDUNA_SECRET_<NAME>}`

---

## What's Not Yet Automated

- MySQL itself must be manually provisioned (this is infrastructure, not application code)
- Emily Prime and Emiree agent processes live in the EMILY repo — their startup scripts are not yet standardised in the same way
- A process supervisor (systemd, supervisor, docker-compose) for keeping all agents running is not yet defined
- The `GOOGLE_CLIENT_ID` for human login must be manually set — human auth is not part of the bootstrap

These are the remaining manual steps. The goal is to reduce them to: (1) provision MySQL, (2) set env vars, (3) run bootstrap. Everything else should follow automatically.
