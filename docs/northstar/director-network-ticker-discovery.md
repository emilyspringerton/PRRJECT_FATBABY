# NORTHSTAR — Director Network Ticker Discovery

**Status:** Draft v0.1 — scoping only, no implementation
**Date:** 2026-07-21
**Founder framing, verbatim:** "expand ticker coverage via the director network."

---

## 1. The idea, plainly

Directors sit on multiple public company boards. If a director we already track (because they
sit on one of our 50 watchlist tickers) also sits on the board of a company we *don't* track, that
company is a real, low-noise candidate for watchlist expansion — better signal than picking
tickers at random, because it's grounded in an actual, real-world relationship our own data
already half-knows about.

## 2. What we actually have today (checked, not assumed)

- 261 director nodes in the graph, built entirely from 8-K Item 5.07 filings (annual-meeting vote
  results) across our current 50-ticker watchlist.
- Only **4** directors currently show up on 2+ of our own 50 tickers — expected, since our
  watchlist is a curated, thematically-unrelated set of large caps, not a natural board-overlap
  cluster.
- **The data this feature actually needs does not exist in our pipeline yet.** Item 5.07 is vote
  tallies — director name, for/against/abstain counts. It does not contain biographical text.
  "Also serves as a director of X, Y, Z" lives in **DEF 14A proxy statements** (the "Nominees"
  section), a filing type `secwatch` does not poll, fetch, or parse at all today — confirmed via
  grep across `secwatch/` and `config/watchlist.json`, zero mentions of "DEF 14A" anywhere.

This is the real finding of this pass: the feature isn't "connect two things we already have," it's
"stand up a new filing-type ingestion pipeline, then build the discovery logic on top of it."
Worth naming honestly before scoping a plan around the wrong assumption.

## 3. What it would actually take

**3a. DEF 14A ingestion** (new, real work) — same shape as `secwatch`'s existing EDGAR polling:
add DEF 14A to the tracked form types, fetch and store as `filing_discovered` /
`source_document_persisted` events, same pattern already proven for 8-Ks. Not a new architecture,
a new form-type parameter on an existing one.

**3b. Bio extraction parser** (new, real work) — DEF 14A director-nominee bios are freeform prose,
not tables (unlike Item 5.07's structured vote data). Extracting "also serves as a director of
[Company]" reliably needs pattern matching against real proxy text (companies phrase this
differently — "currently serves on the board of," "is also a director of," "board member at") —
needs grounding against real sample proxies before committing to a specific regex set, not
invented from imagined phrasing.

**3c. Cross-reference + candidate surfacing** — for each extracted "other directorship," check
whether that company is already on the watchlist (by name — needs fuzzy/canonical matching, company
names in proxy bios won't always match our ticker→name mapping exactly) and if not, surface it as
a discovery candidate. Given `S135`/`S163`'s established pattern for real commerce/vendor
decisions ("don't auto-decide, surface for a human call") — this should almost certainly be a
**proposed list for founder review**, not an auto-add to the watchlist. Adding a ticker means new
ongoing polling cost and changes what the whole pipeline covers; that's a real decision, not a
mechanical one.

## 4. What this explicitly does not do (yet)

Does not ingest DEF 14A today. Does not build the bio parser today. Does not auto-expand the
watchlist under any circumstance — even once built, this produces a candidate list, not an
automatic change. This document is the scoping pass the founder's own framing implied ("expand...
via the director network" is a real, specific mechanism, not a vague direction), not a claim that
any of it is built.

## 5. Sequencing, if picked up

1. DEF 14A ingestion (3a) — the actual prerequisite, real value even standalone (proxy statements
   contain compensation, governance, and voting-recommendation data beyond just bios).
2. Bio parser (3b), grounded against a real sample of fetched proxies once 3a exists — don't build
   the regex before there's real text to test it against.
3. Cross-reference + candidate list (3c), surfaced somewhere a human actually sees it (the
   founder's own status channel — okemily blog report, or a DESKTOP_QUEUE-style entry — not a
   silent auto-add).
