# Ask Emily — GTM Funnel Spec
## FatBaby Signal Intelligence Product | Einhorn Industrial

*Written: 2026-06-12 | Status: spec — no implementation yet*

---

## Product Vision

**Ask Emily** is the consumer-facing intelligence layer on top of the FatBaby signal pipeline.
The pipeline generates structured governance intelligence (board votes, director relationships,
EPS signals, insider transactions, guidance changes) — Ask Emily makes it queryable by humans
and subscribable by institutions.

The product family:
1. **Ask Emily Free** — public query interface for governance signals
2. **Emily+** — subscription tier with premium signals and portfolio alerts
3. **Community Editorial** — user-generated governance commentary alongside machine signals
4. **Merkle Query** — API tier for quantitative funds; pay-per-query with hash-verified data

---

## Tier 1: Ask Emily Free

**What it is:** A search interface over the newssite (`:8082`). Users type a company name or
ticker and get a governance intelligence briefing — board composition, recent signal activity,
vote patterns, insider transactions.

**Content:**
- Last 5 governance signals per company (from the entity graph)
- Board composition (from director signals)
- Recent 8-K filing summary (from processor signals)
- EPS history with guidance changes (from eps-processor)
- Insider transaction summary (from form4-watcher)

**Gate:** Free tier is rate-limited to 10 queries/day per IP. Above that: soft wall with
Emily+ upsell.

**Monetization:** None direct. Audience builder for Emily+.

---

## Tier 2: Emily+

**What it is:** Subscription ($29/month) that unlocks:
- Unlimited queries
- Portfolio watchlist (up to 50 tickers with daily email digest)
- Alert system: push/email when a tracked company files an 8-K, insider sells ≥$500K, or
  director votes against management for the first time
- Full signal history (free tier shows last 30 days; Emily+ shows all-time)
- Jon Agent (`:8084`) options signals — divergence alerts on tracked tickers

**Auth:** IDUNA IAM layer. Emily+ users get a persistent session with `cap.query.full` capability.

**Monetization:** $29/month subscription. Target: 1K subscribers = $29K MRR baseline.

---

## Tier 3: Community Editorial Engine

**What it is:** Each governance signal can have community commentary — sourced from verified
domain experts (analysts, journalists, governance researchers) who create accounts.

**Content model:**
- Each `signal` in the entity graph has a `commentary_thread` — initially empty
- Community members post commentary (markdown, max 1500 chars)
- Editorial team (Emily Prime auto-curated + human review) surfaces top commentary on signal pages
- Emily Prime's own governance commentary (ingest endpoint already exists at
  `/api/v1/ingest/commentary`) posts alongside community voices with "Emily AI" attribution badge

**Monetization:** Engagement loop that justifies Emily+ subscription value. Premium contributors
(verified analysts) get a revenue share on the content they produce that drives Emily+ signups.

**Trust model:** Community commentary requires email verification. Domain expert badge
requires manual review. No anonymous posting.

---

## Tier 4: Merkle Query API

**What it is:** A REST API for quantitative funds and data vendors. Pay-per-query pricing.

**What it returns:** A signed, hash-verified data bundle — each response includes a Merkle
root over the signal set, so the buyer can verify completeness. This is the "Merkle query
monetization" concept.

**Why Merkle verification matters:** Quantitative funds need to prove their data source to
compliance teams. A Merkle-verified signal bundle lets them show: "we queried Ask Emily on
this date, the query returned these signals, and here is the cryptographic proof that the
data has not been modified." This is a SOC 2-adjacent value prop.

**Pricing model:**
- 1000 queries/month: $500/month
- 10K queries/month: $3500/month
- Unlimited: enterprise contract ($25K+/year)

**Endpoints:**
- `GET /api/v1/signals?ticker=AAPL&type=director_vote&since=2026-01-01` → signals + Merkle root
- `GET /api/v1/merkle/verify?root=<hash>` → verification endpoint (public, free)
- `POST /api/v1/export?tickers[]=...&types[]=...` → bulk export with hash manifest

---

## Auth Architecture

```
Free tier:      No auth. Rate limit by IP.
Emily+:         IDUNA session token (JWT). cap.query.full capability.
Editorial:      IDUNA account + email verified. cap.content.post.
Merkle API:     IDUNA API key (M2M). cap.query.api + cap.query.merkle.
```

All tiers route through IDUNA IAM. Emily+ subscription state stored in IDUNA as a `subscription`
resource tied to the account.

---

## Funnel Metrics

| Stage | Metric | Target (Year 1) |
|-------|--------|-----------------|
| Awareness | Unique queries/month (free tier) | 10K |
| Activation | Free → Emily+ conversion | 3% |
| Revenue | Emily+ subscribers | 300 ($8.7K MRR) |
| Expansion | Merkle API customers | 5 ($25K ARR) |
| Retention | Emily+ 12-month retention | 80% |

---

## Implementation Phases

| Phase | What ships | Dependency |
|-------|-----------|-----------|
| Phase 1 | Rate-limited free tier (IP-based) on existing newssite | newssite running ✓ |
| Phase 2 | IDUNA auth integration → Emily+ session gate | IDUNA IAM ✓ |
| Phase 3 | Portfolio watchlist + email digest | EmailServer (Gmail integration) |
| Phase 4 | Community editorial ingestion + display | Ingest endpoint ✓ |
| Phase 5 | Merkle query API + hash verification | New endpoint in signalapi |

Phase 1 can ship in a week — rate limiting on the existing newssite with a paywall page.
