# Competitor Watch: "WEST" (Intrado) — Northstar

## What this is

A northstar for tracking Intrado (internal shorthand: **WEST**) as a named competitor inside
PRRJECT_FATBABY's signal pipeline. Founder direction, real-time (2026-07-24): "northstar WEST opo
(competitor) ingesting court and code data" → "also we need to add west press releases" →
confirmed on follow-up: WEST is **Intrado**, a private company (no ticker, no SEC filings), track
by name only. Logged to `EMILY/BACKLOG.md` S170-20 before this doc was written, per Principle 1.

This is a northstar, not an implementation. Nothing described below is built yet.

## Why Intrado is on the radar

Intrado is an emergency-communications / public-safety tech company (911 routing, NG9-1-1,
GIS/address data for emergency dispatch). The founder's framing — "ingesting court and code
data" — points at the adjacent data Intrado's 911/GIS business depends on: jurisdictional
boundary data (which court/PSAP has authority over a given location) and municipal/address
"code" data (road centerlines, address ranges, zoning) used to route emergency calls correctly.
This is a different shape of data-ingestion competitor than PRRJECT_FATBABY's usual SEC/PR-driven
universe — worth watching as an adjacent-market signal source, not a ticker to trade against.

## Why the existing pipeline doesn't fit as-is

Every existing watcher (`secwatch`, `prwatch`, `form4-watcher`, `dividend-watcher`, etc.) is built
around a **CIK or ticker** key from `config/watchlist.json`. Intrado is private: no CIK, no ticker,
no EDGAR filings, no PR-Newswire wire feed. It publishes its own press releases directly:

- Newsroom root: `intrado.com/news-releases`
- Pagination: `intrado.com/news-releases/page/[1-5]`
- Individual posts: `intrado.com/news-releases/[article-slug]`

Confirmed live 2026-07-24 (most recent post at the time: "Intrado Appoints Damon Covey as Chief
Executive Officer," 2026-07-20).

## Proposed shape (not built)

A new, small watcher — tentatively `cmd/named-competitor-watcher` — that:

1. Polls a configured list of **named competitors** (starting with just Intrado), each defined by
   a direct newsroom URL rather than a CIK/ticker. New config file, e.g.
   `config/named_competitors.json`, parallel to `watchlist.json` but without the SEC-specific
   fields (`cik`, `allowed_forms`).
2. Fetches the newsroom index page, diffs against a seen-slugs cursor (same pattern as
   `observation-watcher`'s `.last-processed` cursor file), and emits a `filing_discovered`-shaped
   event into the existing event store for any new post — reusing the event store and downstream
   processor/signal pipeline rather than building a parallel one.
3. Feeds the same `processor` pipeline used for SEC/PR content, so Intrado press releases get the
   same sentiment/importance/summary treatment as everything else, without a bespoke UI.

## Explicitly out of scope for now

- No attempt to scrape or reverse-engineer Intrado's actual 911/GIS data products or ingestion
  pipeline itself — this tracks their **public announcements**, not their internal data.
- No second named competitor beyond Intrado yet — the config shape should allow more, but only
  Intrado is in scope for the first pass.
- No ticker/ownership research — Intrado is confirmed private; this is a name-based news watch,
  not a financial-signal watch.

## Open question

The founder's original phrase "ingesting court and code data" hasn't been fully unpacked — it's
read here as referring to Intrado's 911/GIS jurisdictional and address-code data ingestion, but
that's an inference, not a confirmed scope statement. If there's a more specific competitive angle
intended (e.g. a specific Intrado product line, or a different company doing literal legal
court-opinion ingestion), flag it and this doc gets revised before Phase 1 build starts.
