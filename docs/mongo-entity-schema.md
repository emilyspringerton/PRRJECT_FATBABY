# FatBaby MongoDB Entity Schema
## Flattened Entity Graph for Ask Emily Queries

**Status:** Spec — implementation tracked in S20-04
**Date:** 2026-06-12
**Why:** The entity-attribute-value graph in memory is rich but not queryable from outside the
process. MongoDB flattens each entity into one JSON document — "give me everything about JPMorgan"
is a single `findOne({ticker: "JPM"})`. No Neo4j ops complexity. No graph traversal needed for
the query patterns Ask Emily uses.

---

## Design Principles

- **One document per tracked entity (company).** Directors, auditors, events all denormalized into
  the company document. Avoids cross-collection joins.
- **Written by entity-graph after each processing run.** Uses `ReplaceOne` with `upsert:true`.
  The whole document is replaced — no partial updates, no merge conflicts.
- **MongoDB is NOT the source of truth.** The eventstore + entity-graph in-process state is.
  MongoDB is a read cache that can be rebuilt from scratch by replaying the eventstore.
- **No Neo4j.** The query patterns are "get all facts about company X" and "find companies with
  auditor changes in Q1 2026" — document queries, not graph traversal.

---

## Document Schema

Collection: `entities`
Key: `ticker` (string, unique index)

```json
{
  "_id": "<auto>",
  "ticker": "JPM",
  "name": "JPMorgan Chase & Co.",
  "entity_type": "company",
  "updated_at": "2026-06-12T14:30:00Z",
  "signal_score": 0.87,

  "directors": [
    {
      "name": "Jamie Dimon",
      "role": "CEO",
      "appointed": "2005-12-31",
      "departed": null,
      "flags": ["activist_target", "compensation_concern"]
    }
  ],

  "auditor": {
    "name": "PricewaterhouseCoopers LLP",
    "since": "2000-01-01",
    "changed_at": null,
    "predecessor": null
  },

  "governance_events": [
    {
      "event_type": "proxy_vote",
      "date": "2026-05-21",
      "headline": "Shareholder vote: say-on-pay passed 78.3%",
      "filing_id": "0000019617-26-000051",
      "signal_score": 0.72
    }
  ],

  "eps_history": [
    {
      "period": "2026Q1",
      "eps_actual": 4.44,
      "eps_estimate": 4.17,
      "surprise_pct": 6.47,
      "report_date": "2026-04-11"
    }
  ],

  "activist_positions": [
    {
      "activist": "ValueAct Capital",
      "shares_pct": 2.1,
      "filing_type": "13D",
      "filed_date": "2026-03-15",
      "status": "active"
    }
  ],

  "watchlist": {
    "enabled": true,
    "tier": "primary",
    "added": "2026-01-15"
  }
}
```

---

## MongoDB Indexes

```javascript
// Primary lookup
db.entities.createIndex({ ticker: 1 }, { unique: true })

// Query: find all companies with high signal score
db.entities.createIndex({ signal_score: -1 })

// Query: find all companies with recent auditor changes
db.entities.createIndex({ "auditor.changed_at": -1 })

// Query: find all companies with active activist positions
db.entities.createIndex({ "activist_positions.status": 1 })

// Query: find by director name across companies
db.entities.createIndex({ "directors.name": 1 })
```

---

## Writer Pattern (for S20-04 implementation in entity-graph)

In `cmd/entity-graph/main.go`, after each processing run:

```go
// After processing all events, upsert entity documents to MongoDB.
func writeEntitiesToMongo(ctx context.Context, db *mongo.Database, graph *EntityGraph) error {
    coll := db.Collection("entities")
    for _, entity := range graph.Entities() {
        doc := buildEntityDocument(entity)
        filter := bson.M{"ticker": entity.Ticker}
        opts := options.Replace().SetUpsert(true)
        if _, err := coll.ReplaceOne(ctx, filter, doc, opts); err != nil {
            return fmt.Errorf("upsert %s: %w", entity.Ticker, err)
        }
    }
    return nil
}
```

Activated only when `MONGODB_URL` is set (graceful degradation — entity-graph runs without MongoDB).

---

## signalapi Entity Endpoint (for S20-05 implementation)

```
GET /api/v1/entities/{ticker}
```

Handler: connect to MongoDB, `findOne({ticker: ticker})`, return JSON.
If MongoDB not configured: return 503 with `{"error": "entity graph not available"}`.

---

## Local Dev Setup

```bash
# Start MongoDB (Docker)
docker run -d --name mongo -p 27017:27017 mongo:7

# Set env vars
export MONGODB_URL=mongodb://localhost:27017
export MONGODB_DB=fatbaby

# Run entity-graph with MongoDB write enabled
./entity-graph --store var/secwatch
# → writes entity documents after each processing batch
```

See `docs/local-dev-setup.md` (S20-06) for full local dev runbook.
