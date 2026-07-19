# NORTHSTAR: Auto-Generated Daily Articles

**Status:** Draft v0.1
**Date:** 2026-07-19
**Owner:** Emily Springerton
**Founder framing, verbatim:** "ok lets back step our way into auto gen articles like at 945am
every day we want to post a list of stocks on the moove we will need to augment our data
ingestion to power this" — followed by four more data-source asks in the same session (oil,
central bank, investor calls, market calendar), captured here as one plan rather than four
separate half-built pipelines.

---

## 1. The shape of this

One daily flagship piece — **"Stocks on the Move,"** published ~9:45am ET on market days only —
plus a small set of new data feeds that either power it directly or are natural siblings once the
scheduling/gating infrastructure exists. "Back step our way into" means: build the boring
infrastructure first (calendar gating, one clean ingestion pattern), prove it on the simplest
feed, then layer the rest on top of the same shape rather than inventing a new pattern per feed.

## 2. What's already decided

- **Movers scope: true market-wide gainers/losers**, not just our 50-ticker watchlist (founder
  decision, 2026-07-19). Source: Yahoo Finance's own free/unofficial screener endpoint
  (`query1.finance.yahoo.com/v1/finance/screener/predefined/saved?scrIds=day_gainers` /
  `day_losers`) — same vendor already integrated (`cmd/market-data-watcher`), no new API key, no
  new cost. Tradeoff, stated plainly: movers outside our 50-ticker watchlist have no SEC
  filing/press-release context from our own pipeline (no signals, no commentary) — those entries
  in the article are numbers-only (ticker, company name, % change, volume) unless a lightweight
  company-description lookup is added later.
- **Market-day gating is mandatory** — "dont try to post a moovers article on memorial day etc."
  Shipped: `internal/marketcal` (`IsMarketDay`, `HolidayName`, `IsEarlyClose`), computed from
  NYSE's published holiday rules (not a hardcoded per-year table), PRRJECT_FATBABY `2619192`.

## 3. Data sources — status and what each needs

| Feed | Source | New credential needed? | Status |
|---|---|---|---|
| Market-wide movers | Yahoo screener (`day_gainers`/`day_losers`) | No — same free/unofficial API `market-data-watcher` already uses | Not started |
| Market calendar (gating) | Computed, NYSE holiday rules | No | **Shipped** (`internal/marketcal`) |
| Oil/petroleum data | EIA (U.S. Energy Information Administration) API v2, `api.eia.gov/v2/` | **Yes** — free self-serve key at eia.gov/opendata/register.php (email only, no OAuth/browser-consent flow, unlike Gmail) | Not started |
| Fed/FOMC press releases | Federal Reserve RSS: `federalreserve.gov/feeds/press_monetary.xml` | No — public RSS, no key | Not started |
| FOMC meeting dates | Published in advance on federalreserve.gov (fixed annual schedule, ~8 meetings/year) | No | Not started |
| Investor/earnings conference calls | **No source picked yet** — distinct from the existing `internal/earningscal` (which tracks report *date* + BMO/AMC only, no dial-in/webcast/call-time info). Needs research: most likely either scraping company IR pages or a paid calendar API (e.g. Nasdaq's earnings calendar has call times for some tickers; a dedicated conference-call data vendor may be needed for full coverage) | Founder decision pending | Not started |

## 4. Sequencing

**Phase 0 — Calendar gating.** Done. `internal/marketcal`, PRRJECT_FATBABY `2619192`.

**Phase 1 — Stocks on the Move (the flagship, and the pattern for everything after it).**
1. New `cmd/movers-watcher` (or extend `market-data-watcher`): pulls Yahoo's `day_gainers` +
   `day_losers` screener once daily around 9:30–9:45am ET (gated by `marketcal.IsMarketDay`),
   emits a `market_movers_snapshot` event (ticker, name, % change, price, volume) into the event
   store — same append-only pattern as every other watcher in this pipeline.
2. New commentary `Kind` value on the existing (already-built, never-yet-used)
   `internal/newssite/commentary` package: `"market_movers"`. `commentary.Article` and
   `commentary.Append` already exist — this is genuinely the "one clean pattern" moment, since
   nothing has actually written through this package yet (`cmd/emily-agent`'s
   `fatbaby_publish_commentary` tool is wired for it but has zero real callers found in this
   audit).
3. Article generation: for watchlist tickers among the movers, pull real context (recent signals,
   filings) the way `emily_commentary` articles already do; for non-watchlist movers, numbers-only
   per §2. Whether this is LLM-authored prose (matching "auto gen articles" framing) or a
   templated table is a real design call for when this phase is actually built — not decided here.
4. Scheduling: systemd timer (`OnCalendar=`), same idiom as `earnings-alert`'s existing weekly
   timer (`emily.cli/cmd/start.go`'s `runInstallEarningsAlert`) — gate the actual run on
   `marketcal.IsMarketDay(time.Now())` inside the binary, not just the timer's own calendar
   syntax, so a timer misfire or manual run on a holiday still no-ops correctly.
5. Publish target: newssite (this is FatBaby-domain financial content, not OKEMILY blog content)
   — likely a new `/section/movers` or reusing the existing commentary rendering path. Confirm
   against `docKicker`/`sourceTypeLabel` (`internal/newssite/render.go`) once `Kind` values are
   finalized.

**Phase 2 — EIA oil/petroleum data.** Blocked on founder registering a free API key
(`eia.gov/opendata/register.php`) — queued in `EMILY/docs/DESKTOP_QUEUE.md`. Once a key exists:
new `cmd/eia-watcher` following the exact `market-data-watcher` shape (poll, emit event, no new
architecture). Specific series IDs (weekly crude stocks, WTI/Brent prices, etc.) to be picked once
this phase actually starts.

**Phase 3 — Fed/FOMC.** Two distinct pieces: (a) ingest `press_monetary.xml` on a poll (same
shape as `prwatch`, just RSS instead of an API), (b) a small fixed calendar of published FOMC
meeting dates (same idea as `marketcal`, but sourced from the Fed's published schedule rather than
computed — meeting dates aren't rule-based like holidays, they're announced).

**Phase 4 — Investor/earnings conference calls.** Not scoped yet — needs a source decision
(founder or research pass) before any code. Real question to resolve first: is this extending
`internal/earningscal` with call-time/dial-in fields, or a genuinely separate feed?

**Phase 5 — Bond/treasury data.** Shipped 2026-07-19. `internal/bonddata` fetches FRED's free,
no-API-key CSV export (2Y/10Y/30Y treasury yields, high-yield corporate spread) — same "no new
vendor, no cost" shape as Phase 1's Yahoo dependency. `cmd/bond-watcher` records a daily snapshot
via systemd timer (6pm ET, after FRED's typical same-day publication), gated on
`marketcal.IsMarketDay`. Live-verified: all 4 tracked series recorded against the real FRED API.
Data-ingestion only so far — no article/display surface yet (no `/section/bonds` page, no
commentary integration), same "ship the data layer, surface it later" sequencing as EIA/Fed.

## 5. What this explicitly does not do yet

Phase 0 (market calendar) and Phase 1 (movers) and Phase 5 (bonds) are live. Phases 2-4 (EIA,
Fed, investor calls) remain unbuilt. This document exists so rapid-fire asks in one session land
as one sequenced plan instead of abandoned half-starts — per "back step our way into," foundation
and plan first, each phase built and verified on its own.
