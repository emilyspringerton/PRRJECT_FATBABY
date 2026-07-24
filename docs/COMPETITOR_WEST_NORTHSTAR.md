# Competitor Watch: "WEST" (West Publishing / Thomson Reuters) — Northstar

## What this is

A northstar for tracking Thomson Reuters' West Publishing business (internal shorthand: **WEST**)
as a competitor inside PRRJECT_FATBABY's signal pipeline. Founder direction, real-time
(2026-07-24): "northstar WEST opo (competitor) ingesting court and code data" → "also we need to
add west press releases" → an initial identity guess (Intrado) was raised, tried, confirmed by the
founder, then corrected back: "not intrado do WEST publishing court data." Logged to
`EMILY/BACKLOG.md` S170-20 before/alongside this doc, per Principle 1 — including the identity
back-and-forth itself, not just the final answer.

This is a northstar, not an implementation beyond the one concrete, low-risk step described below
(adding the parent company to the existing watchlist).

## Who WEST actually is

**West Publishing** is the legal-publishing division of **Thomson Reuters Corporation**
(NYSE/TSX: `TRI`), best known for **Westlaw**, one of the two dominant legal-research platforms
(alongside LexisNexis). Its core business is exactly what the founder described: ingesting and
organizing **court opinions/case law** ("court data") and **statutes/regulations** ("code data")
at massive scale, then selling access to lawyers and researchers — plus the historical West
"key number" case-classification system that's been central to U.S. legal research for over a
century. West doesn't trade separately; it's wholly owned by Thomson Reuters, so tracking it
means tracking the parent.

## What changed from the first draft of this doc

The first pass of this northstar guessed **Intrado** (a private 911/emergency-communications
company) based on a "private, track by name only" answer during scoping. That answer has been
superseded by the founder's direct correction. Intrado is **not** part of this competitor watch —
this doc replaces that content rather than layering on top of it, to avoid leaving a stale,
wrong lead in a golden doc.

## Why the existing pipeline fits here (unlike the Intrado guess)

Unlike Intrado, Thomson Reuters is **public** and already files with the SEC (foreign private
issuer forms — `6-K`, `40-F`, not `10-K`/`10-Q`, since it's a Canadian company) under
**CIK 1075124**. That means it fits the existing `secwatch`/`prwatch` CIK-based pipeline exactly
as-is — no new "named competitor" watcher needed, unlike what the (incorrect) Intrado guess would
have required.

**Done, this pass:** added `TRI` to `config/watchlist.json` (6-K/40-F, sector
"legal/information services", poll priority 3) — this is the concrete "add west press releases"
ask, now served by the existing pipeline rather than a bespoke build.

## What's still just a northstar (not built)

- **Westlaw-specific signal extraction.** The existing pipeline will now catch Thomson Reuters'
  own corporate filings/press releases (earnings, M&A, leadership), but it has no special handling
  to surface Westlaw/legal-tech-specific news buried inside those (e.g. a new AI legal-research
  product launch) versus the rest of Thomson Reuters' businesses (Reuters News, tax/accounting
  software, etc.). A future pass could add keyword-tagged filtering scoped to the legal-services
  segment specifically.
- **Direct competitive framing for FatBaby's own roadmap.** No FatBaby feature currently competes
  with Westlaw's court-opinion/case-law ingestion directly — this is being tracked as market
  intelligence (what a dominant player in "ingest structured public-record data at scale" is doing)
  rather than because FatBaby is entering legal research. Worth revisiting if that changes.
- **Connection to S170-21 (lawsuit-filing alerts).** The founder's separate, real-time ask for
  "alerts when big time lawsuits get filed against public companies" sits right next to this
  thread conceptually — West/Westlaw is exactly the kind of company that already sells that
  capability. If FatBaby ever builds real docket-level lawsuit detection, Westlaw's product
  approach (and PACER/state-court integration patterns generally) is the natural prior art to
  study, not reinvent blind. See S170-21 in `EMILY/BACKLOG.md` for that item's own scope, which is
  intentionally kept separate from this one for now.
