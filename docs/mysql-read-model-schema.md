# FatBaby MySQL Read Model Schema
## CQRS Projection Layer for Ask Emily Product

**Status:** Spec — implementation tracked in S20-02 through S20-05
**Date:** 2026-06-12
**Why:** The eventstore is append-only and correct but cannot serve ad-hoc queries for the
newssite/Ask Emily product. These MySQL tables are CQRS read models — projections from the
eventstore. The eventstore remains the source of truth; MySQL is the query layer.

---

## Design Principles

- **Append-only events → mutable read models.** The projector tails the eventstore and upserts.
- **One table per query pattern.** Don't try to normalize; optimize for the reads Ask Emily makes.
- **MongoDB for graph, MySQL for flat relational.** Entity graph → MongoDB (see mongo-schema.md).
  MySQL handles: per-ticker governance events, EPS results, signal timelines.
- **Projector is idempotent.** Can replay from cursor 0 and produce the same state.
- **Schema migrated via numbered SQL files** (same pattern as IDUNA's `migrations/truestore/`).

---

## Tables

### 1. `governance_signals`

Powers: "Show me all governance events for JPMorgan in Q1 2026"

```sql
CREATE TABLE IF NOT EXISTS governance_signals (
    id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    ticker          VARCHAR(20)     NOT NULL,
    event_type      VARCHAR(64)     NOT NULL COMMENT 'auditor_change|director_nomination|eps_surprise|activist_13d|proxy_vote|...',
    filing_id       VARCHAR(128)    NOT NULL COMMENT 'EDGAR accession number or PR source ID',
    entity_name     VARCHAR(255)    NOT NULL DEFAULT '' COMMENT 'director/auditor/activist name if applicable',
    signal_score    FLOAT           NOT NULL DEFAULT 0 COMMENT 'strategic relevance 0.0–1.0',
    headline        VARCHAR(512)    NOT NULL DEFAULT '',
    raw_json        JSON            NULL COMMENT 'full signal payload for Ask Emily context',
    filing_date     DATE            NOT NULL,
    ingested_at     TIMESTAMP(6)    NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    eventstore_seq  BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT 'source event sequence number — dedup key',
    PRIMARY KEY (id),
    UNIQUE KEY uq_seq (eventstore_seq),
    INDEX idx_ticker_date (ticker, filing_date DESC),
    INDEX idx_ticker_type (ticker, event_type),
    INDEX idx_event_type (event_type),
    INDEX idx_filing_date (filing_date DESC),
    INDEX idx_signal_score (signal_score DESC)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

### 2. `eps_results`

Powers: "What were Apple's last 4 quarters EPS vs estimate?"

```sql
CREATE TABLE IF NOT EXISTS eps_results (
    id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    ticker          VARCHAR(20)     NOT NULL,
    period          VARCHAR(16)     NOT NULL COMMENT 'e.g. 2026Q1 (fiscal quarter)',
    eps_actual      DECIMAL(10,4)   NULL COMMENT 'reported EPS',
    eps_estimate    DECIMAL(10,4)   NULL COMMENT 'consensus estimate at time of report',
    surprise_pct    DECIMAL(8,4)    NULL COMMENT '(actual-estimate)/|estimate| * 100',
    revenue_actual  DECIMAL(18,2)   NULL COMMENT 'total revenue in USD',
    report_date     DATE            NOT NULL,
    filing_id       VARCHAR(128)    NOT NULL DEFAULT '',
    eventstore_seq  BIGINT UNSIGNED NOT NULL DEFAULT 0,
    ingested_at     TIMESTAMP(6)    NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_ticker_period (ticker, period),
    INDEX idx_ticker_date (ticker, report_date DESC),
    INDEX idx_report_date (report_date DESC),
    INDEX idx_surprise (surprise_pct)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

### 3. `entity_timeline`

Powers: "Give me everything that changed about Goldman Sachs board in 2026"

```sql
CREATE TABLE IF NOT EXISTS entity_timeline (
    id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    ticker          VARCHAR(20)     NOT NULL,
    entity_name     VARCHAR(255)    NOT NULL COMMENT 'director/auditor/activist/company name',
    entity_type     VARCHAR(32)     NOT NULL COMMENT 'director|auditor|activist|company',
    event_type      VARCHAR(64)     NOT NULL COMMENT 'appointed|resigned|changed|flagged|...',
    event_date      DATE            NOT NULL,
    role            VARCHAR(128)    NOT NULL DEFAULT '' COMMENT 'CEO|CFO|Board Member|Auditor|...',
    description     TEXT            NULL COMMENT 'human-readable event summary for Ask Emily',
    source_filing   VARCHAR(128)    NOT NULL DEFAULT '' COMMENT 'EDGAR accession number',
    eventstore_seq  BIGINT UNSIGNED NOT NULL DEFAULT 0,
    ingested_at     TIMESTAMP(6)    NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_seq (eventstore_seq),
    INDEX idx_ticker_date (ticker, event_date DESC),
    INDEX idx_ticker_entity (ticker, entity_name),
    INDEX idx_entity_type (entity_type, event_type),
    INDEX idx_event_date (event_date DESC)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

### 4. `projector_cursors`

Tracks the eventstore cursor position per projector so replay is restartable.

```sql
CREATE TABLE IF NOT EXISTS projector_cursors (
    projector_name  VARCHAR(64)     NOT NULL COMMENT 'e.g. governance_signals|eps_results|entity_timeline',
    last_seq        BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT 'last processed eventstore sequence number',
    updated_at      TIMESTAMP(6)    NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (projector_name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

---

## Projector Pattern (for S20-02 implementation)

```
Startup:
  1. Read projector_cursors WHERE projector_name = 'governance_signals'
  2. If no row exists, start from seq = 0
  3. Open eventstore from that seq, tail in 2s poll loop

Per event:
  1. Deserialize event type
  2. If event_type matches projection (e.g. "signal_extracted"):
     a. Build row from event payload
     b. INSERT ... ON DUPLICATE KEY UPDATE (upsert on eventstore_seq)
     c. UPDATE projector_cursors SET last_seq = ?
  3. Skip events not relevant to this projector

Error handling:
  - Log + skip malformed events (do not crash; log to Apple observation)
  - On MySQL connection failure: backoff + retry, do not advance cursor
```

---

## signalapi Query Endpoints (for S20-05 implementation)

```
GET /api/v1/signals
  ?ticker=BAC
  &type=auditor_change
  &since=2026-01-01
  &until=2026-06-12
  &limit=50
  → []governance_signals row

GET /api/v1/eps/{ticker}
  ?periods=4
  → []eps_results rows (last N periods)

GET /api/v1/timeline/{ticker}
  ?since=2026-01-01
  &entity_type=director
  → []entity_timeline rows

GET /api/v1/entities/{ticker}
  → MongoDB entity document (see mongo-schema.md)
```

---

## MongoDB Entity Schema (for S20-03/04 — separate doc)

Entity graph data goes to MongoDB, not MySQL:
- One document per tracked entity (company/director/auditor)
- Denormalized: all facts about the entity in one JSON document
- See `docs/mongo-entity-schema.md` (to be written in S20-03)

---

## Migration Files (for S20-02 implementation)

Create under `migrations/mysql/` (new directory, mirrors IDUNA's truestore pattern):
```
migrations/mysql/
  202606120001_governance_signals.sql
  202606120002_eps_results.sql
  202606120003_entity_timeline.sql
  202606120004_projector_cursors.sql
```

---

## Environment Variables (for S20-06 local dev setup)

```
MYSQL_URL          — DSN: user:pass@tcp(localhost:3306)/fatbaby
MYSQL_PROJECTOR    — comma-separated list of projectors to run (default: all)
MONGODB_URL        — mongodb://localhost:27017 (for entity graph)
MONGODB_DB         — fatbaby (default)
```
