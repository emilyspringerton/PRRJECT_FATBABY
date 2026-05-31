# Changelog

All notable changes to this project are documented in this file.

## 2026-05-31 — Test coverage: docindex, graphread, handler routes; editorial-rules config

### Test coverage (Emily observation follow-through)

Verified the date provenance bug reported in `var/emily-observations/latest.json` is
fully resolved. Added tests that would catch a regression:

- **`internal/newssite/docindex/docindex_test.go`** (new): 14 tests covering Ingest
  (skip non-doc, dedup by identity, lowercase ticker normalization, skip empty identity),
  ForTicker ordering (newest-first), Recent (limit, cap-at-count), KnownTickers (sorted),
  LatestSeq tracking, Build round-trip against a real file store, previewText truncation.
- **`internal/newssite/graphread/graphread_test.go`** (new): 12 tests covering Refresh
  (empty dir, signals, nodes with ticker index, auditors, bidirectional edges), LiveSignals
  (expiry filter, ticker filter, all-tickers shortcut), SignalsForPerson canonicalization,
  Updates channel closed on each Refresh, AllTickers, RulesUpdatedAt zero when unconfigured.
- **`internal/newssite/handler_test.go`**: 18 new cases covering every route (healthz, wire,
  archive, about, tickers, live, breaking, section/governance, section/earnings, api/tickers,
  search, ticker, ticker RSS), plus two date-provenance cases that verify the 2019 AAPL
  filing shows `2019` in the detail page body and is suppressed from the front page feed.

### Editorial rules config

- **`config/editorial-rules.json`** (new): Documents the newssite editorial decisions
  (historical badge threshold: 90 days, front-page suppression, date display rules).
  Machine-readable so Emily and Claude Code can reference and adjust thresholds without
  reading source code.

### Northstar cleanup

- **`docs/northstar/newssite.md`**: P4 section header updated from "🔄 In progress" to
  "✅ Complete" — all P4 items were already implemented and checked.

## 2026-05-31 — Filing date provenance fix + newssite front-page performance

### Filing date provenance (Emily observation)

Signals written before the FilingDate field was added to source_document_persisted
payloads had no filing_date, so the newssite fell back to detected_at (2026-05-30),
making 2019 AAPL proxy filings appear as breaking news.

- **`internal/entitygraph/graph.go`**: Added `RewriteSignals` for atomic backfill rewrites.
- **`cmd/entity-graph/main.go`**: Builds a filing-date index from all `filing_discovered`
  events at batch start and uses it to recover the correct SEC date when a source doc
  lacks `filing_date`. This is a stable fallback: filing_discovered events always carry
  the correct EDGAR date.
- **`cmd/backfill-signal-dates`**: One-shot command that patches existing signals.ndjson
  records by looking up correct dates from filing_discovered events. Run once to repair
  historical data; 18 AAPL 2019 signals backfilled to 2019-03-04.

### Newssite front-page performance (4.5s → sub-millisecond)

Every front-page request was reading and deserializing the entire 60 MB event store
because `file_store.ReadFrom` loads a full NDJSON file before filtering by sequence,
and `ReadLatest` called it on every HTTP request.

- **`internal/newssite/docindex`**: Extended `DocSummary` to carry `FilingDate`,
  `BodyPreview`, `DocumentURL`, and `CharCount` — all fields needed for front-page and
  archive rendering. Added `Recent(n)` method for in-memory newest-N retrieval.
- **`internal/newssite/handler.go`**: `serveFrontPage`, `serveWire`, `serveArchive`, and
  `serveTicker` now prefer the in-memory docindex over raw event-store reads when the
  index is wired. Falls back to `ReadLatest` if docindex is not configured (e.g., tests).
  Added `summaryToEntry` converter so the ticker page also gets `FilingDate` and
  `BodyPreview` from the index path.

## 2026-05-31 — ReadLatest reverse-scan optimization; epsread and reader tests

### ReadLatest backward-walk (reader.go)

`ReadLatest` previously scanned the entire event store forward from sequence 1 and
reversed the result — O(N) where N is total store events. For a store with 100K events,
fetching the latest 50 documents required reading all 100K events.

Replaced with a backward-walk from `LatestSequence()` in chunks of 512. The walk stops
as soon as `limit` results are found. For a typical running system (where recent events
are predominantly `source_document_persisted`), this reduces reads by 99%+ and keeps
front-page latency flat as the store grows.

The `reverse` helper is removed (no longer needed). Two new tests assert the newest-first
ordering and limit-capping behaviour against a real file store.

### epsread tests

Four tests covering the new package:
- `TestRefresh_Empty` — graceful no-op on missing articles.ndjson
- `TestRefresh_LoadsAndSortsNewestFirst` — verifies PublishAt desc ordering
- `TestArticlesFor_FiltersByTicker` — per-ticker lookup + case-insensitive normalization
- `TestRecent_CapAtN` — limit respected, limit=0 returns empty

## 2026-05-31 — EPS articles integrated into newssite; EPS + runbook docs updated

### EPS article integration

The `eps-processor` has been generating `var/eps/articles.ndjson` for every qualifying
press release since it was built, but those articles were invisible to the newssite. This
change closes the loop.

- **`internal/newssite/epsread/epsread.go`** — new read model that loads `articles.ndjson`
  from `var/eps/` into memory with 60s background refresh. `Recent(n)` and
  `ArticlesFor(ticker)` lookups; `Count()` for startup logging.
- **`internal/newssite/render.go`** — `EarningsItemView` view type + `ToEarningsItemView` /
  `EarningsItemsFrom` converters (import `internal/eps`). `EarningsSectionView` for
  `/section/earnings`. `Earnings []EarningsItemView` added to `FrontPageView` and
  `TickerPageView`. `RenderEarningsPage` function.
- **`internal/newssite/handler.go`** — `epsStore *epsread.Store` field + `SetEpsStore`;
  `recentEPS(n)` helper. `serveEarnings` handler. Front page passes 4 latest EPS articles
  to `RenderListPage`. Ticker page passes per-ticker articles to `RenderTickerPage`.
  Route `/section/earnings` (checked before the generic `/section/` prefix matcher).
- **`internal/newssite/templates.go`** — `.kicker-earnings` CSS. Earnings sidebar box on
  front page (shows headline, ticker, EPS amount, period, link to original doc). Earnings
  section on ticker page (period + GAAP label). Full `/section/earnings` page template.
  "Earnings" added to the sections-rail nav on every page.
- **`cmd/newssite/main.go`** — `-eps-dir` flag (default `var/eps`); epsread.Store wired
  and refreshed at startup; `SetEpsStore` called on handler.

### Docs

- `docs/headlines/eps-implementation.md` — status updated from "Implementation Framework"
  to "Operational"; roadmap replaced with implementation status table; newssite integration
  documented.
- `docs/headlines/eps.md` — status updated to operational.
- `docs/news-site-e2e-runbook.md` — removed stale "files from previous Codex sessions"
  prerequisite list; repo already builds cleanly.

## 2026-05-31 — Newssite P4 final: accessibility pass + northstar complete

### Accessibility improvements

- `role="banner"` on `<header>` in masthead; `<main>` landmark on detail page (was `<div>`).
- `role="search"` on search form; `<label for="q">` + `aria-label` on search input and submit
  button so screen readers can identify the search widget.
- `aria-label="Site sections"` on sections-rail nav; `aria-label="Document navigation"` on
  back-nav links — distinguishes the two nav landmarks on multi-nav pages.
- Heading hierarchy: breaking page and section page titles promoted from `<h2>` to `<h1>`;
  article headlines remain `<h2>` so the page outline reads h1 → h2 correctly.
- `.sr-only` utility class added to CSS (visually hidden, screen-reader accessible).
- `/live` added to sections-rail so the live desk is reachable from every page nav.

### Northstar complete

All newssite northstar phases (P0–P4) and newssite-tickers phases (T1–T4) are now fully
implemented. Newssite `newssite.md` status updated to reflect completion.

## 2026-05-31 — Newssite P4: per-ticker RSS, corrections box, stale-doc cleanup

### Per-ticker RSS

- `/ticker/{symbol}/feed.xml` — live governance signal feed per ticker; items use `FilingDate`
  as `<pubDate>`; `<link rel="alternate">` autodiscovery added to every ticker page `<head>`.
- `RenderTickerRSS` function; `serveTickerRSS` handler; route catches `HasPrefix("/ticker/") &&
  HasSuffix("/feed.xml")` before the existing `/ticker/` page route.

### Corrections box

- `graphread.Store.rulesFile` — configurable via `Store.SetRulesFile(path)`; stat'd on each
  `Refresh()` call to track mtime.
- `Store.RulesUpdatedAt()` returns the last observed rules-file modification time.
- `/about` page: when `RulesUpdatedAt` is set, shows "Methodology recently updated" banner (amber
  colour) if rules changed within the last 14 days, otherwise shows the update date as a plain
  note. The text explains that signals scored before the update date used the prior rule version.
- `cmd/newssite/main.go` calls `gs.SetRulesFile("config/entity-graph-rules.json")` so the about
  page automatically reflects rule changes made by the Emily → Claude Code loop.

### Northstar doc cleanup

- Data-exposure table updated: all surfaces now marked ✅ (governance signals, people, board
  relationships, auditor records, ticker rollups, live stream all exposed on the site).
- Sitemap `/live` entry corrected to ✅.
- `newssite-tickers.md` T4 per-ticker RSS marked complete.

## 2026-05-31 — Newssite P3+P4: /live SSE page, succession-watch rail, handler test coverage

### /live SSE page (P3 completion)

- **`/live`** — renders current critical/high signal list server-side (fully works without JS)
- **`/live/events`** — SSE endpoint; `connected` on handshake, `refresh` on each graphread
  Store refresh, `heartbeat` comment every 30s to survive proxies
- **`graphread.Store`** — `updates chan struct{}` closed and replaced on each `Refresh()`; exposed
  via `Store.Updates()` so any future handler can wait for new signal data
- **JS enhancement** (~25 lines inline) — `EventSource('/live/events')` diff-prepends new cards
  fetched from `/breaking` on each refresh event; CSS fadeIn; fully degrades without JS

### Succession-watch rail (P4)

- `SuccessionWatchItem` view type; `SuccessionWatch []SuccessionWatchItem` on `FrontPageView`
- `buildFrontPage` populates from ranked `director_decay` signals (up to 5, by rank)
- Front-page sidebar: director name → `/person/`, ticker, approval %, brief deck

### Test coverage

New handler test cases: person page without graph (404), feed.xml, section feed, company redirect.

## 2026-05-31 — Newssite P2+P4: director dossier page, RSS feeds, front-page historical filtering

### /person/{canonical_id} director dossier (P2 completion)

The last P2 route — the full navigation flow (front page → ticker → director → another ticker)
now works end to end with zero URL typing.

- **`internal/entitygraph/graph.go`** — `LoadEdgesFromDir`: edges were writable but never
  loadable on startup; added the missing load method so board co-membership edges survive restart.
- **`internal/newssite/graphread/graphread.go`** — load edges at `Refresh()` time and build an
  `edgesByNode` reverse index (canonical_id → all edges involving that person). Added three new
  methods: `AllNodes()`, `EdgesFor(canonicalID)`, `SignalsForPerson(canonicalID)` — the last uses
  `entitygraph.Canonicalize` for matching so hyphenation/punctuation variants never break a lookup.
- **`internal/newssite/render.go`** — `PersonPageView`, `BoardEntryView`, `AppearanceView`,
  `InterlockView`, `SparklineView` view models. `buildPersonPage` assembles vote history grouped
  by ticker (oldest-to-newest), signals filtered by canonical entity ID, board co-directors from
  the edge index. `buildSparkline` emits SVG `<polyline>` points scaled to the 60–100% approval
  range with a dashed friction-threshold line at 90%.
- **`internal/newssite/templates.go`** — `personTemplate`: broadsheet layout with person header,
  sparkline, per-ticker vote-breakdown tables (for/against/abstain/broker-NV formatted as 1.4B/
  221M), signals rail, and interlock grid. All director names already linked to `/person/` from
  the ticker page template.
- **`internal/newssite/handler.go`** — `/person/{canonical_id}` route; `servePersonPage` handler.

### RSS feeds (P4)

- **`/feed.xml`** — front-page RSS 2.0 feed; items use `FilingDate` as `<pubDate>` so dates
  are accurate after the date-provenance fix.
- **`/section/{slug}/feed.xml`** — per-desk RSS feeds (governance, activism, boardroom, etc.)
- `<link rel="alternate" type="application/rss+xml">` autodiscovery tag in front-page `<head>`.

### Front-page historical filtering (backfill flood protection)

Signals and documents whose `filing_date` is more than 90 days old are now suppressed from the
front-page feed. Historical articles still appear on ticker pages and section pages, but get an
"ARCHIVE ·" kicker prefix and "Indexed DATE" byline footnote so readers know when the pipeline
discovered the filing vs. when the filing was made.

### Signal date provenance fix

- `Signal.FilingDate` field added; populated from SEC filing metadata (not pipeline processing
  timestamp) by `ScoreDirectorVotes`, `ScoreProposals`, and `ScoreAuditorChange`.
- `cmd/entity-graph/main.go` prefers `doc.FilingDate` over `doc.PersistedAt`; the old code
  stamped all backfill-era documents with today's date, making 2019 filings appear as breaking news.
- `edition.buildDateline` uses `FilingDate` as primary display date, `DetectedAt` as footnote.

## 2026-05-31 — Northstar doc cohesion pass

All northstar documents updated to reflect the system as it actually is rather than as it was
planned:

- `northstar.md` — all four pipeline layers marked operational; phases 1–3 marked complete;
  phase 4 (regulatory/lobbying enrichment) marked partial; file paths corrected to actual layout.
- `executive_summary.md` — wrong file names fixed throughout; document inventory table with
  completion markers added; status updated from "ready to implement" to "operational".
- `newssite.md` — per-route ✅/❌ status in sitemap; phase completion checklist replacing
  future-tense narrative; `/company/` → `/ticker/` rename noted.
- `newssite-tickers.md` — T1–T3 complete; T4 broken into specific done/outstanding items.
- `quick_start.md` — complete rewrite as operational runbook (how to start processes, inspect
  signals, tune rules, troubleshoot) replacing the "build from scratch" guide.
- `lvl2/` docs — research-notes headers added explaining these are exploratory political-
  intelligence notes, not active FATBABY implementation specs.
- `agent/task.md` — marked completed (entity-graph is in Emily's process registry).

## 2026-05-30 — Self-improvement loop iteration: processor form bug + entity-graph filter + richer prompts

Three bugs blocking the entity-graph → observation-watcher → Claude loop:

- **`secwatch/discovery.go`** — add `EffectiveForm()` method to `FilingDiscoveredEvent`:
  `FilingDiscovered` events (written by secwatch) use JSON field `"form_type"`, but
  `FilingDiscoveredEvent` (read by processor) had `Form string json:"form"` — the name
  mismatch meant `filing.Form` was always `""`. Added `FormType string json:"form_type"` and
  `EffectiveForm()` which returns whichever field is populated.

- **`internal/processor/worker.go` + `persist_source.go`** — use `filing.EffectiveForm()` instead
  of `filing.Form` throughout. Previously all documents were stored as `source_type="press_release"`
  with `form=""` even for genuine 8-K filings, making them invisible to entity-graph.

- **`cmd/entity-graph/main.go`** — widen the 8-K detection: accept `doc.Form == "8-K"` OR
  `doc.SourceType == "sec_8k"` OR URL contains "8-K" (fallback for the 1104 historical documents
  already stored with the wrong labels). Add `seenSourceDocs` counter and a WARNING log when
  source documents are seen but 0 pass the 8-K filter. Add `strings` import. Reset cursor to 1
  so all 1104 historical documents are reprocessed (37 are genuine 8-K filings detectable by URL).

- **`cmd/observation-watcher/main.go`** — rewrite `buildGenericPrompt` to include `suggested_fix`
  and `findings` directly in the prompt body instead of just saying "read the file". Claude now
  gets actionable context in the prompt itself with numbered instructions.

Root cause chain: secwatch writes `"form_type"` → processor reads `"form"` (empty) →
`kind="press_release"` always → entity-graph filter `form=="8-K"` drops everything → 0 signals →
Emily's observation was also being gate-suppressed (fixed in prior commit).

## 2026-05-30 — Recursive self-improvement: observation-watcher gate fix + eps-processor logging

- **`cmd/observation-watcher/main.go`** — fix `isTrivialObservation`: generic Emily observations
  with `severity=error` (or any non-empty, non-"ok" severity) were classified as trivial because
  the function only checked entity-graph structured fields (`status`, `gaps`, `parse_errors`,
  `signals_by_type`), all of which are empty for hand-written health observations. Added severity
  check at the top of `isTrivialObservation` so any observation with `severity != "" && != "ok"`
  bypasses the gate and triggers Claude. Added regression test `TestPollOnceTrivialGateAllowsErrorSeverity`.
- **`cmd/eps-processor/main.go`** — add WARNING log when ticker map has < 10 entries after load,
  making the "all PRs discovered without tickers" state visible rather than silent. Add per-release
  `no_ticker` log line when a press release body is processed without a ticker in the discovery map.

Root cause of gate failure: `latest.json` embeds `source`/`status`/`gaps` inside the `findings`
free-text string rather than as structured JSON fields; only `severity` is a reliable structured
signal in Emily's hand-written observations.

## 2026-05-30 — Newssite P0+P1: broadsheet redesign, signal-based front page, 7 new routes

- **`internal/newssite/render.go`** — complete rewrite: `html/template`-based rendering with typed view
  models; `ArticleView` with `Link` field supporting both signal-derived and document articles;
  `buildFrontPage` merges ranked governance signals with source documents; signals take the lead
  and secondary slots when present, docs fill the rest.
- **`internal/newssite/templates.go`** — new broadsheet stylesheet (Georgia serif body / system-ui
  furniture, severity palette, hairline rules, two-column grid); templates for front page, detail,
  breaking, section, company desk, archive, about/masthead.
- **`internal/newssite/graphread/graphread.go`** — new package: loads `var/entity-graph/signals.ndjson`
  + `nodes.ndjson` into memory; `LiveSignals(ticker, today)` filters expired signals; background
  refresh via `StartRefresh`.
- **`internal/newssite/edition/edition.go`** — new package: `Rank(signals, today)` scores and sorts
  by `severity_weight × confidence × recency_decay`; `GenerateHeadline` implements all 13 signal-type
  templates from the northstar; `SectionFor` routes signal types to desk slugs.
- **`internal/newssite/edition/edition_test.go`** — 20 golden tests covering headline generation for
  all signal types, ranking order, expiry filtering, and deduplication.
- **`internal/newssite/handler.go`** — 7 new routes: `/wire`, `/breaking`, `/section/{slug}`,
  `/company/{ticker}`, `/archive`, `/about`, `/healthz`; optional `SetGraphStore` wires in signals.
- **`cmd/newssite/main.go`** — added `-graph-dir` flag (default `var/entity-graph`); starts
  background graphread refresh every 30s.

## 2026-05-30 — Emily capability expansion: governance + EPS ops tools, full pipeline coverage

- **`cmd/emily-agent/main.go`** — 7 new tools + expanded process coverage:
  - `fatbaby_start_process` now knows: `entity-graph`, `eps-processor`, `eps-reconciler`, `schd13-watcher`, `observation-watcher`, `signalapi`, `feedserver` (was: 6 processes).
  - `fatbaby_process_status` now checks all 13 pipeline processes (was 6) + store dirs for entity-graph, eps, schd13.
  - `fatbaby_run_entity_graph_once` — one-shot entity-graph batch (processes 8-K votes → signals → observation, ~30-90s). Requires -one-shot flag added to entity-graph.
  - `fatbaby_run_schd13_once` — one-shot Schedule 13D/13G poller (queries EDGAR, updates accuracy records).
  - `fatbaby_run_eps_reconcile_once` — one-shot EPS reconciler (matches pending oracle cases against filed 8-K EPS).
  - `fatbaby_eps_status` — oracle precision dashboard: pending/confirmed/contradicts counts, precision score, article count.
  - `fatbaby_count_press_releases` — counts pr_discovered + pr_body_fetched + pr_body_failed in prwatch stores.
  - System prompt completely rewritten: 3 roles (ops/governance/EPS), full pipeline architecture diagram, startup sequences for all sub-pipelines, governance observation reading guide, EPS analyst rules, updated tick prompt.
- **`cmd/emily-agent/signal_intelligence.go`** — `fatbaby_signal_summary` expanded:
  - Governance health index: most recent `governance_health_index` score per ticker, sorted worst-first.
  - Accuracy scores: reads `var/entity-graph/accuracy.ndjson`, derives per-signal-type precision.
  - High/critical alerts now exclude governance_health_index (it's in its own table).
- **`cmd/entity-graph/main.go`** — added `-one-shot` flag: run one batch and exit. Enables Emily's `fatbaby_run_entity_graph_once` tool and cron-based operation without a long-running daemon.
- **19 registered tools total (was 12), all packages passing.**

## 2026-05-30 — EPS Phase 1: oracle store, article generation, eps-processor, eps-reconciler

- **`internal/eps/oracle.go`** (new): Oracle store persistence for the EPS self-improvement loop.
  - `LoadOracleCases(dir)` — reads `var/eps/oracle.ndjson`; returns nil when file absent.
  - `AppendOracleCase(dir, c)` — appends one case (creates file if needed).
  - `WriteOracleCases(dir, cases)` — atomic rewrite via temp-file rename; used when updating verdicts.
  - `OracleSummary` — aggregates confirmed/contradicts/pending counts + precision score for the feedback loop.
  - `BuildOracleSummary(cases)` — derives precision = confirmed/(confirmed+contradicts).
- **`internal/eps/article.go`** (new): Article generation per northstar editorial spec.
  - `Article` type: Headline, Dek, Body, SourceIdentity, Ticker, Period, EPSValue, IsGAAP, PublishAt.
  - `Generate(e *EarningsData) (Article, bool)` — returns `false` when confidence < 0.70 or FlagTraceFailure set (gating before publish).
  - Headline rules: GAAP positive → `"{Issuer} reports {period} EPS of ${N.NN}"`; GAAP loss → `"... loss of ${N.NN} per share"`; adjusted-only → `"... adjusted EPS of ${N.NN}"`.
  - Dek includes YoY change % and revenue when available. Body includes all extracted fields.
  - `formatPeriodLabel` — handles Q1–Q4, FY, year-only, unknown.
- **`internal/eps/oracle_test.go`** (new): 5 tests — round-trip append/load, missing file returns nil, atomic rewrite, summary precision, all-pending → precision=0.
- **`internal/eps/article_test.go`** (new): 7 tests — AAPL headline contains 2.40/Q1 2026, MSFT uses GAAP not adjusted, loss headline format, low-confidence blocked, trace-failure blocked, period label formatting, body contains Revenue.
- **`cmd/eps-processor/main.go`** (new): Pipeline process — polls `var/prwatch-body` for `pr_body_fetched` events.
  - Loads `pr_discovered` events from `var/prwatch` into a ticker map (discovery ID → primary ticker symbol).
  - For each body event: runs `eps.Extract` + `eps.Validate`; skips if no EPS found.
  - Publishes: writes `Article` to `var/eps/articles.ndjson` + pending `OracleCase` to `var/eps/oracle.ndjson`.
  - Cursor at `var/eps-processor/.cursor`; flags: `-discovery-store`, `-body-store`, `-eps-dir`, `-poll-interval`, `-one-shot`.
- **`cmd/eps-reconciler/main.go`** (new): Async oracle reconciler — scans `var/secwatch` for 8-K `source_document_persisted` events that look like Item 2.02 earnings releases.
  - Filters 8-Ks with EPS language (`item 2.02`, `earnings per share`, `diluted earnings`, `per share`).
  - Runs `eps.Extract` on the 8-K body; matches to pending oracle cases by `ticker:quarter:year`.
  - Within 5% relative error → `VerdictConfirmed`; otherwise → `VerdictContradicts`. Logs delta.
  - Rewrites `var/eps/oracle.ndjson` atomically after each pass; logs precision summary.
  - Flags: `-store`, `-eps-dir`, `-poll-interval` (default 6h), `-one-shot`.
- **31 eps tests, all passing. 17 packages passing.**

## 2026-05-29 — Cycle 7: retrospective accuracy tracking + Schedule 13D/13G watcher (Phase 4)

- **`internal/entitygraph/accuracy.go`** (new): Retrospective accuracy infrastructure.
  - `GroundTruth` type: `confirmed` / `refuted` / `pending`.
  - `AccuracyRecord` — maps a signal prediction to its real-world outcome; persisted in `var/entity-graph/accuracy.ndjson`.
  - `AccuracyReport` — per-signal-type summary: total/confirmed/refuted/pending/precision.
  - `Schd13Filing` — representation of an SEC Schedule 13D/13G filing; persisted in `var/schd13/filings.ndjson`.
  - `CorrelateActivistRisk(signals, filings)` — for each `activist_risk` signal checks whether an SC 13D was filed within the prediction window [DetectedAt, ValidThrough]. 13D filed in-window → confirmed; window expired with no 13D → refuted; window still open → pending. SC 13G (passive holder) and pre-signal filings do not confirm.
  - `BuildAccuracyReports(records)` — aggregates records into summary reports; precision = confirmed/(confirmed+refuted).
  - `LoadSchd13Filings` / `WriteSchd13Filings` — NDJSON store at `var/schd13/filings.ndjson`.
  - `LoadAccuracyRecords` / `WriteAccuracyRecords` — NDJSON store at `var/entity-graph/accuracy.ndjson`.
- **`internal/entitygraph/accuracy_test.go`** (new): 8 tests covering confirmed/refuted/pending classification, 13G passive-holder exclusion, pre-signal filing exclusion, multi-ticker isolation, precision calculation with all-pending inputs.
- **`cmd/schd13-watcher/main.go`** (new): EDGAR poller for Schedule 13D/13G filings.
  - Reads `config/watchlist.json` (enabled entries only); queries EDGAR EFTS full-text search (`efts.sec.gov/LATEST/search-index`) for SC 13D/13G/amendments referencing each target CIK in the last N days.
  - Appends new filings to `var/schd13/filings.ndjson`.
  - After writing, loads all historical signals and runs `CorrelateActivistRisk` → writes accuracy records → logs precision summary per signal type.
  - Flags: `-watchlist`, `-graph-dir`, `-out-dir`, `-lookback` (days, default 90), `-poll-interval` (default 6h), `-one-shot`, `-dry-run`.
- **Observation gains `accuracy_scores`** (`internal/entitygraph/observer.go`): `AccuracyScores []AccuracyReport` field added. When accuracy records exist, the observation reports signal precision so Claude/Emily can see whether activist_risk predictions are being validated by real 13D filings.
- **`cmd/entity-graph/main.go`** wired: `-schd13-dir` flag (default `var/schd13`). After composite scoring, loads `filings.ndjson` from schd13-dir, runs `CorrelateActivistRisk` against all historical + current signals, writes accuracy records, and passes reports to `BuildObservation`.
- **`BuildObservation` signature updated**: Added `accuracyReports []AccuracyReport` parameter (pass nil when no records).
- **44 tests, all passing.**

## 2026-05-29 — Cycle 6: governance_health_index, filing date fix, gap detector cleanup, signal tracing

- **New signal: `governance_health_index`** (`internal/entitygraph/signals.go`, `rules.go`, `config/entity-graph-rules.json`, `cmd/entity-graph/main.go`):
  - `ScoreGovernanceHealth(ticker, allSignals, windowDays)` — composite health score [0.0, 1.0] derived from all signals in the trailing window. Penalties: nomination_rejection(-0.40), entrenchment(-0.30), activist_risk(-0.25), auditor_change(-0.20), friction(-0.20), comp_concern(-0.15), decay/family/director_link(-0.10 each), BNV/abstention(-0.05 each). Bonus: +0.05 per high_trust director, capped at +0.20. SCHW 2026 pattern scores ~0.30 (severity: high). A clean five-director board with no adverse signals scores ~1.0 (low). The score gives a single rankable number for cross-company governance comparisons.
  - `governance_health_window_days` (default 365) added to `Rules` with hot-reload support.
  - Wired in `cmd/entity-graph/main.go` after `director_link` scoring; fires per ticker in batch using `combined` signals.
  - Added to `AllSignalTypes` so it zero-fills in observations.
  - 5 new tests: healthy board (score > 0.80, low severity), SCHW-like adverse combo (score ≤ 0.45, high), no-signals → nil, cross-ticker isolation, nomination_rejection+entrenchment combo.
- **Bug fix: filing date accuracy** (`cmd/entity-graph/main.go`): Changed `filingDate := time.Now().UTC()` to `doc.PersistedAt.Format("2006-01-02")` (zero-value fallback to today). The same filing now always receives the same canonical date regardless of when entity-graph processes it. Previously, re-runs on different days could create duplicate `FilingAppearance` records with different dates, corrupting the multi-filing history that `director_decay` relies on.
- **Fix: detectGaps false positives** (`internal/entitygraph/observer.go`): Removed three gap checks that fired incorrectly for healthy non-family companies: "No friction signals on 4+ director board", "No entrenchment despite N proposals parsed", and "No family control signals". These conditions are expected for well-governed Phase 3 companies (GS, MS, C, IBKR) and were causing spurious `needs_attention` observations. Only genuine parsing failures remain: `nodeCount == 0` (parser completely missed directors) and `proposalsProcessed == 0` despite having directors (proposal-splitter failure).
- **Signal source tracing** (`cmd/entity-graph/main.go`): Per-filing signals (director votes, proposals) now carry `source_identity = doc.Identity` in their `Metadata` map. Enables retrospective accuracy tracking: a future process can look up which filing triggered a given signal. Composite signals (activist_risk, director_link, governance_health) are multi-source and remain untagged.
- **35 tests, all passing.**

## 2026-05-29 — Cycle 5: director centrality, observer gate, compaction + README

- **Director centrality** (`internal/entitygraph/graph.go`): Added `Centrality int` field to `PersonNode`. Recomputed in `UpsertPerson` after every filing append as the count of distinct tickers in the node's filing history. A director with `Centrality >= 3` is a bridge node — their friction signals propagate risk across multiple companies. Field is persisted in `nodes.ndjson` and surfaced in `fatbaby_entity_graph` tool output.
- **Graph compaction** (`internal/entitygraph/graph.go`): Added `CompactNodes(dir string) error` — rewrites `nodes.ndjson` in place, keeping only the last record per `canonical_id`. The append-only store accumulates duplicates across runs; compaction runs automatically at the start of each `entity-graph` batch (`cmd/entity-graph/main.go`).
- **Observer status fix** (`internal/entitygraph/observer.go`): `status` field now correctly set to `needs_attention` when there are **gaps** (not just parse errors). Previously gaps were silent — the observation was `ok` even when the pipeline had unresolved coverage issues.
- **Observation-watcher severity gate** (`cmd/observation-watcher/main.go`): Added `-gate` flag (default `nontrivial`, env `OBSERVATION_GATE`). When `nontrivial`, the watcher skips Claude invocation when the observation has: `status=ok`, no parse errors, no gaps, and only `high_trust_director` signals. The cursor is still updated (deduplicated) — just no Claude API call. Prevents burning quota on quiet high-trust-only batches. `gate=none` restores always-invoke behaviour. Added two new tests covering the gate in both directions.
- **README rewrite**: Complete overhaul. Now accurately describes the entity-graph intelligence engine, recursive self-improvement loop, Emily's dual ops+analyst role, signal type glossary, northstar roadmap, and runtime data layout.

## 2026-05-29 — Emily signal intelligence

- **`cmd/emily-agent/signal_intelligence.go`** (new): Three new tools giving Emily the ability to read and explain entity-graph governance signals.
  - `fatbaby_signal_summary` — high-level dashboard: total signals, counts by type and severity, top tickers by signal count, most recent high/critical alerts with full interpretations. No params; the right first call for "what's going on?"
  - `fatbaby_query_signals` — filtered search with `ticker`, `signal_type`, `min_severity`, `days`, `limit` params. Results sorted by severity then date. Returns matching signals with full interpretation text.
  - `fatbaby_entity_graph` — director and company graph lookup. `ticker` param returns all directors at a company with approval trends and co-board partners. `director` param does a partial-name search and returns approval trend (declining/improving/stable with Δ pp) plus co-board relationships from edge data. No params = graph-wide stats (node count, edge count, top cross-company directors, known auditors).
  - All three tools read `var/entity-graph/{signals,nodes,edges,auditors}.ndjson` directly; deduplication matches `LoadNodesFromDir` / `LoadSignals` logic from the entitygraph package.
- **Emily system prompt extended** (`cmd/emily-agent/main.go`): Added analyst role alongside ops role. Signal type glossary with plain-English interpretation of all 12 signal types. Signal analyst operating rules: always fetch live data before answering, synthesise into opinionated assessment (severity, cause, recommended action), explain activist_risk as a composite, treat director_link as forward-looking propagation warning.

## 2026-05-29 (recursive self-improvement cycle 4)

- **`activist_risk` composite signal** (`internal/entitygraph/signals.go`, `graph.go`, `rules.go`, `config/entity-graph-rules.json`, `cmd/entity-graph/main.go`):
  - `ScoreCompositeActivistRisk(ticker, allSignals, windowDays)` — fires when `governance_entrenchment` AND (`director_friction` | `nomination_rejection`) co-occur at the same ticker within `activist_risk_window_days` (default 365). Score = `1 - worst_director_approval` (Herringer at 84.3% → score 0.157). High severity, 0.82 confidence. Signal_id includes date for append-log uniqueness.
  - `LoadSignals(dir string)` added to `graph.go` — reads `signals.ndjson` into memory for cross-batch composite scoring.
  - `cmd/entity-graph/main.go`: historical signals loaded at batch start; composite scores run after per-filing loop using `combined = historical + current`; `collectTickers()` helper returns the set of tickers in the current batch.
  - `ActivistRiskWindowDays int` added to `Rules` struct (hot-reloadable, default 365).
  - For SCHW 2026: both entrenchment (92% vote blocked by 80% supermajority) and friction (Herringer 84.3%) fire from the same 8-K, so `activist_risk` fires on the first real run.
- **`director_link` Phase 3 signal** (`internal/entitygraph/signals.go`, `cmd/entity-graph/main.go`):
  - `ScoreDirectorLinks(graph, allSignals)` — for each friction/rejection director, finds other tickers in their node's `Filings` and emits a `director_link` signal for each. Signal carries `source_ticker` and `source_signal` in metadata for traceability.
  - Returns nil today (Herringer only in SCHW graph); will fire automatically as GS/MS/C/IBKR filings accumulate and the graph finds shared directors.
  - Wired in `main.go` after decay and activist-risk scoring.
- **7 new tests**: `ScoreCompositeActivistRisk` fires/no-fire/stale-signal cases; `ScoreDirectorLinks` single-ticker (no fire) and multi-ticker (fires for IBKR) cases.
- **Seed observation updated**: 11 signals (activist_risk now firing for SCHW); gaps narrowed to director_link activation (needs multi-company data), CIK verification for disabled fintech tickers, and observation-watcher severity gate proposal.

## 2026-05-29 (recursive self-improvement cycle 3)

- **`auditor_change` signal — full pipeline** (`internal/entitygraph/parser.go`, `graph.go`, `signals.go`, `cmd/entity-graph/main.go`):
  - Parser: added `reAuditorName` regex (`appointment of <Firm LLP> as`) and `reRatificationProposal` detector. `extractAuditor()` scans proposal chunks for ratification language, returns firm name. `Item507Result.Auditor` carries it out of the parser. The SCHW 2026 fixture correctly yields `"Deloitte Touche LLP"`.
  - Graph: `AuditorRecord` struct; `Graph.Auditors map[string]*AuditorRecord`; `TrackAuditor()` returns `(changed bool, prevFirm string)`; `LoadAuditorsFromDir()` / `FlushAuditors()` backed by `var/entity-graph/auditors.ndjson`. First record per ticker is stored without emitting a signal; change is detected on subsequent filings.
  - Signal: `ScoreAuditorChange(ticker, prev, new)` emits medium-severity `auditor_change` with `prev_auditor`/`new_auditor` metadata.
  - `main.go`: auditors loaded and flushed alongside nodes/edges; `TrackAuditor` called after every 8-K parse; signal appended if changed.
  - 6 new tests: auditor extraction from SCHW fixture, no-ratification case, `TrackAuditor` first/change/no-change, `ScoreAuditorChange` field validation.
- **`broker_nonvote_anomaly_threshold` recalibrated** (`config/entity-graph-rules.json`): Lowered 0.15 → 0.12. SCHW's BNV fraction (~14.2%) was below the old threshold despite being structurally elevated for a retail brokerage. The new threshold makes the signal fire for SCHW-class BNV levels without being so sensitive that every small-cap triggers it.
- **Phase 3 watchlist expansion** (`config/watchlist.json`): Added GS (886982), MS (895421), C (831001), IBKR (1383312) as enabled; COIN (1679273), SOFI (1818502), HOOD (1783398) added as `enabled: false` pending CIK verification against live EDGAR submissions endpoint. Watchlist grows from 17 → 25 entries.
- **Seed observation updated**: Reflects 9 signals from SCHW (broker_nonvote_anomaly now firing); auditor_change noted as "first run, no prior record"; cycle 4 request targets composite signal `post_entrenchment_activist_risk` and CIK verification for disabled fintech entries.

## 2026-05-29 (recursive self-improvement cycle 2)

- **New signal: `nomination_rejection`** (`internal/entitygraph/signals.go`, `rules.go`, `config/entity-graph-rules.json`): Critical-severity signal for directors who fail to receive majority support (approval < `nomination_rejection_threshold`, default 50%). Under majority voting standards common in S&P 500 companies, sub-50% approval obligates the director to submit a resignation; board refusal is itself a governance crisis indicator. The signal is mutually exclusive with `director_friction` (the switch case is ordered: rejection → friction → high-trust). Added `NominationRejectionThreshold` to `Rules` with hot-reload support. Added test verifying mutual exclusivity with friction.
- **Wire `ScoreDirectorDecay`** (`cmd/entity-graph/main.go`): The decay signal function existed since Phase 1 but was never called. Added `scoreDecayFromGraph` helper that iterates the in-memory graph after each batch, groups each node's filings by ticker, sorts by date, and calls `ScoreDirectorDecay` for any director with 2+ appearances at the same company. Adds `"sort"` import.
- **`signals_by_type` zero-fill** (`internal/entitygraph/signals.go`, `observer.go`): Added `AllSignalTypes` slice listing all 10 known signal types. `BuildObservation` pre-fills `byType` map with zero counts for each, so the observation always shows the complete signal coverage picture regardless of which signals fired. Downstream Claude prompts can now distinguish "evaluated but didn't fire" from "not yet implemented".
- **Updated seed observation** (`var/emily-observations/latest.json`): Reflects the expected output of a fully-fixed run on the SCHW 2026 8-K: 5 directors, 3 proposals, 7 signals (director_friction×1, high_trust×4, family_control×1, governance_entrenchment×1). All 10 signal types now shown with zero-fills. Gaps describe the actual remaining work: decay needs multi-filing history, auditor_change needs schema extension, Phase 3 needs watchlist expansion.

## 2026-05-29 (recursive self-improvement cycle 1)

- **Parser robustness** (`internal/entitygraph/parser.go`): Strengthened three regexes for live EDGAR text variance: (1) `reSupermajority` now matches "80% of outstanding shares" without requiring "the" (`(?:all\s+)?(?:the\s+)?` optional); (2) `reOutstandingShares` now also handles "N common shares outstanding" and "N shares of stock outstanding"; (3) `proposalSplitter` now matches proposal numbers 10–19 and uses a word-boundary anchor so "Proposal 4:" splits correctly. These fix the seed observation gap about missing entrenchment signals.
- **New signal: `abstention_spike`** (`internal/entitygraph/signals.go`, `rules.go`, `config/entity-graph-rules.json`): Implements the northstar spec vote-pattern signal. Fires when proposal abstention rate exceeds `abstention_spike_threshold` (default 10%). Added `AbstentionSpikeThreshold` to `Rules` struct with hot-reload support and two new tests.
- **Observation diagnostics** (`internal/entitygraph/observer.go`, `cmd/entity-graph/main.go`): Added `proposals_processed` field to `Observation`. `detectGaps` now distinguishes "proposals not found in text" (parser failure) from "proposals found but no signal fired" (threshold issue). `request_for_claude` explicitly flags the proposal-splitter as the failure point when 0 proposals are parsed. `BuildObservation` signature updated to accept `proposalsProcessed int`.

## 2026-05-29

- Added `internal/entitygraph` package: Phase 1 of the northstar 8-K Intelligence Engine. Parses SEC 8-K Item 5.07 vote results (director nominee names, for/against/abstain/broker-non-vote counts, approval percentages) including compound hyphenated surnames (e.g. "Schwab-Pomerantz"). Builds a PersonNode entity graph stored as NDJSON, generates co-appearance edges between board co-members, and emits six governance signal types: `director_friction`, `high_trust_director`, `director_decay`, `family_control`, `governance_entrenchment`, and `broker_nonvote_anomaly`. Signal thresholds are hot-reloadable from `config/entity-graph-rules.json` — Claude Code can modify this file as part of the recursive self-improvement loop.
- Added `cmd/entity-graph`: new pipeline process that polls the secwatch event store for `source_document_persisted` 8-K events, runs the entity-graph extraction pipeline, appends to `var/entity-graph/{nodes,edges,signals}.ndjson`, and publishes an `Observation` to `var/emily-observations/` after each batch. The observation-watcher picks this up to close the Emily ↔ Claude Code feedback loop. Cursor persisted at `var/entity-graph/.cursor` for resumable processing.
- Added `config/entity-graph-rules.json`: baseline signal scoring thresholds (friction ≤85%, high-trust ≥95%, entrenchment min-for ≥80%, comp-concern against ≥30%, family-name keywords). Intended to be refined by Claude Code suggestions without a process restart.
- Added test fixture `fixtures/entitygraph/schw_8k_5_07_2026.txt`: representative SCHW 2026 annual meeting 8-K with Item 5.07 vote data (5 directors including Marianne C. Brown, Frank C. Herringer, Carolyn Schwab-Pomerantz; board declassification supermajority failure), used by `internal/entitygraph` tests.

## 2026-05-28
- Fixed processor failing to fetch every filing with `unsupported protocol scheme ""`. The SEC submissions feed gives `primaryDocument` as a bare filename (e.g. `d259921d8k.htm`); `ParseRecentFilings` was storing it directly into `Filing.PrimaryDocument`, so the processor's `http.Get` had no scheme. Added `secwatch.DocumentURL(cik, accession, primaryDocument)` and call it at parse time, so every filing's `PrimaryDocument` is now the fully-qualified `https://www.sec.gov/Archives/edgar/data/<cik>/<accession-no-dashes>/<filename>` URL. The processor is unchanged.
- Fixed `FilingDiscovered` event payload missing the top-level `ticker` field, which made the processor see `ticker=""` for every filing. Added `Ticker` to `FilingDiscovered` and populated it from `Filing.Ticker` in `discoveryEventData`. `FilingDiscoveredEvent` (the processor-side shape) was already correct and remains untouched.
- Existing `var/secwatch` events were written with bare-filename `primary_document` values; delete `var/secwatch` and re-run `secwatch` to re-seed with correct URLs.
- Created the canonical `./var/` directory skeleton (`secwatch`, `prwatch`, `prwatch-body`, `logs`, `emily-observations`) so pipeline processes can write to the paths CLAUDE.md and `cmd/emily-agent` already expect.
- Added `.gitignore` covering runtime data (`var/`, `data/`, `bar.bak/`, stray `cmd/*/var/`) so pipeline runs no longer pollute `git status`.
- Added `fatbaby_write_observation` and `fatbaby_read_observation` tools to `cmd/emily-agent` so Emily can publish structured findings to `var/emily-observations/latest.json` — the handoff file for the Emily ↔ Claude Code feedback loop. Extended Emily's system prompt to describe when to use them.
- Added `cmd/emily-agent/observation_test.go` covering the new write/read observation tools: latest+archive emission, default severity, required-field validation, and round-trip read.
- Added `cmd/observation-watcher`, the trigger half of the Emily ↔ Claude Code feedback loop. It polls `var/emily-observations/latest.json`, persists a timestamp cursor at `.last-processed`, and shells out to `claude --dangerously-skip-permissions` (overridable) with a prompt referencing the observation. Updated CLAUDE.md to document it.
- Added `POST /tick` to `cmd/emily-agent` so an external scheduler (cron, systemd timer, GitHub Actions) can ask Emily to do an unattended health sweep and publish an observation only when warranted. Refactored `Server` to allow injecting the Anthropic URL so the tick handler is testable end-to-end via `httptest`.

## 2026-05-21
- Added a TCP feed server with framed protocol and session streaming support.
- Added a tenant-aware broker proxy with hot-reload registry support.
- Expanded the README with full financial signal pipeline architecture and usage details.
- Added a second construct workflow with reduced fixture footprint.
- Added a signal API server with indexed eventstore read model for querying signals.
- Added Emily fatbaby operations tool support and moved the Emily agent entrypoint under `cmd`.
- Added unified discovery identity schema with early ticker extraction support.
- Added distributed event intelligence architecture documentation.
- Added requested SEC watchlist tickers.
- Added a resilient `fatstream` TCP client SDK plus CLI and usage examples.
- Fixed broker registry merge issues and unified shared routes configuration.

## 2026-05-16
- Added `prwatch` crawler entrypoint and `prwatch-body` command implementations.
- Converted crawler implementation into reusable `prwatch` library components.
- Updated construct bundle workflow configuration.

## 2026-05-14
- Added processor pipeline stage logging for event flow debugging.
- Improved startup logging, including data directory path reporting and log formatting fixes.

## 2026-05-12
- Merged controlled historical backfill strategy work.

## 2026-05-11
- Added configurable polling loop for continuous SEC discovery.
- Added processor worker pipeline and intelligence signal model.
- Added PR Newswire watcher with scraper client and runner.
- Added realtime dashboard server with SSE-based UI.
- Added documentation for Track A historical backfill strategy.

## 2026-04-26
- Added SEC watchlist discovery command with safe polling.

## 2026-04-22
- Added corpus-wide SEC fixture harness and invariants tests.

## 2026-04-21
- Added project README with setup and usage guidance.
- Added CI workflow for deterministic construct artifact generation.
- Added fixture files to construct bundles.
- Added broad SEC fixture corpus harness and smoke tests.

## 2026-04-20
- Initial repository setup.
- Implemented file-backed NDJSON event store with contract specs and demo.
