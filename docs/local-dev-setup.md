# FatBaby Local Dev Setup — MySQL + MongoDB

**Status:** Operational runbook — S20-06
**Date:** 2026-06-12

This document covers running MySQL and MongoDB locally for the Ask Emily query layer.
Both databases are optional read caches — the eventstore remains the source of truth.

---

## Quick Start (Docker)

```bash
# MySQL (port 3306)
docker run -d \
  --name fatbaby-mysql \
  -e MYSQL_ROOT_PASSWORD=fatbaby \
  -e MYSQL_DATABASE=fatbaby \
  -e MYSQL_USER=fatbaby \
  -e MYSQL_PASSWORD=fatbaby \
  -p 3306:3306 \
  mysql:8.4

# MongoDB (port 27017)
docker run -d \
  --name fatbaby-mongo \
  -p 27017:27017 \
  mongo:7
```

Wait ~5 seconds for MySQL to initialize before running the projector.

---

## Environment Variables

```bash
# MySQL — projector + signalapi
export MYSQL_URL="fatbaby:fatbaby@tcp(localhost:3306)/fatbaby"

# MongoDB — entity-graph + signalapi
export MONGODB_URL="mongodb://localhost:27017"
export MONGODB_DB="fatbaby"         # default if not set
```

Add these to your shell profile or `.env` file.

---

## Running the Projector (MySQL)

The projector (`cmd/projector`) tails the secwatch eventstore and writes to MySQL.
It applies migrations automatically at startup.

```bash
# From PRRJECT_FATBABY root:
export MYSQL_URL="fatbaby:fatbaby@tcp(localhost:3306)/fatbaby"
export FATBABY_ROOT=.

go run ./cmd/projector \
  --store var/secwatch \
  --poll-interval 2s

# One-shot (process current backlog and exit):
go run ./cmd/projector --store var/secwatch --one-shot
```

On first run the projector creates 4 tables in MySQL:
- `governance_signals` — one row per signal_generated event
- `eps_results` — EPS data (populated by eps-reconciler when wired)
- `entity_timeline` — named-entity change events
- `projector_cursors` — restart cursor per projector

---

## Running entity-graph with MongoDB

```bash
export MONGODB_URL="mongodb://localhost:27017"
export MONGODB_DB="fatbaby"

go run ./cmd/entity-graph \
  --store var/secwatch \
  --one-shot

# → Writes entity documents to MongoDB entities collection after each batch
# → Log line: "mongowriter upserted=N tickers=M"
```

---

## Running signalapi with MySQL + MongoDB

```bash
export MYSQL_URL="fatbaby:fatbaby@tcp(localhost:3306)/fatbaby"
export MONGODB_URL="mongodb://localhost:27017"
export MONGODB_DB="fatbaby"

go run ./cmd/signalapi --addr :9091

# Startup log shows:
# MySQL connected: governance-signals + eps endpoints enabled
# MongoDB connected db=fatbaby: entities endpoint enabled
```

### Query endpoints

```bash
# Governance signals for a ticker
curl "http://localhost:9091/v1/governance-signals?ticker=JPM&limit=10"

# EPS history for a ticker
curl "http://localhost:9091/v1/eps/AAPL?periods=4"

# Full entity document from MongoDB
curl "http://localhost:9091/v1/entities/JPM"
```

---

## Resetting / Reseeding

```bash
# Reset MySQL (destroys all projection data — safe to do, replay from eventstore)
docker exec fatbaby-mysql mysql -ufatbaby -pfatbaby fatbaby \
  -e "DROP TABLE IF EXISTS governance_signals, eps_results, entity_timeline, projector_cursors;"

# Replay all events from cursor 0
go run ./cmd/projector --store var/secwatch --one-shot
# → projector_cursors has no row → starts from seq 0

# Reset MongoDB (destroys entity cache)
docker exec fatbaby-mongo mongosh fatbaby --eval "db.entities.drop()"

# Re-populate MongoDB entity graph
go run ./cmd/entity-graph --store var/secwatch --one-shot
```

---

## docker-compose (optional)

```yaml
# docker-compose.yml (save at PRRJECT_FATBABY root)
services:
  mysql:
    image: mysql:8.4
    environment:
      MYSQL_ROOT_PASSWORD: fatbaby
      MYSQL_DATABASE: fatbaby
      MYSQL_USER: fatbaby
      MYSQL_PASSWORD: fatbaby
    ports:
      - "3306:3306"
    healthcheck:
      test: ["CMD", "mysqladmin", "ping", "-h", "localhost", "-ufatbaby", "-pfatbaby"]
      interval: 5s
      timeout: 3s
      retries: 10

  mongo:
    image: mongo:7
    ports:
      - "27017:27017"
```

```bash
docker-compose up -d
# Wait for mysql healthcheck to pass, then:
export MYSQL_URL="fatbaby:fatbaby@tcp(localhost:3306)/fatbaby"
export MONGODB_URL="mongodb://localhost:27017"
go run ./cmd/projector --one-shot
go run ./cmd/entity-graph --one-shot
go run ./cmd/signalapi
```

---

## Troubleshooting

| Problem | Fix |
|---------|-----|
| `dial tcp: connect: connection refused` on MySQL | MySQL not started or not ready yet. Try again in 5s. |
| `auth failure` MySQL | Check MYSQL_URL has correct user:pass |
| `server selection timeout` MongoDB | MongoDB not started. `docker start fatbaby-mongo` |
| Projector starts from 0 every time | `projector_cursors` table empty. That's correct first-run behavior. |
| signalapi shows 503 on `/v1/entities/{ticker}` | MONGODB_URL not set, or MongoDB not reachable. |

---

## See Also

- `docs/mysql-read-model-schema.md` — SQL DDL + projector pattern
- `docs/mongo-entity-schema.md` — MongoDB document schema
- `migrations/mysql/` — numbered SQL migration files (applied automatically)
