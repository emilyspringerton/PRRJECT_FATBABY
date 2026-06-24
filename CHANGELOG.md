# Changelog

## 2026-06-24
- feat: S126-12 velocity alerts — signalindex.CheckVelocity + GET /v1/velocity-alerts, >3 signals <1h importance>60, 5 tests (Apple #3561)
- feat: S126-11 entity co-occurrence graph — cooccurrence.Store, GET /v1/entities/{ticker}/related, SeedFromSignals startup warm-up (Apple #3557)

- feat: S125-09 signal confidence scoring + /v1/data-quality endpoint — A–F grade per ticker, source trust, completeness, quant bonus, 18 tests (Apple #3518)


## 2026-06-23
- S107: signal replay engine + CLI (cmd/replay) — replay filing_discovered events through haiku/stub provider, stream Signal JSONL, filter by date window
- S103-01: ArchetypeProvider in internal/processor — THE_FIELD spirit-stack routing for SEC signal analysis; -archetype-engine flag in cmd/processor; 9 tests
- signalapi poll-interval default 2s→30s (-73% CPU); stub-backfill cmd (haiku re-analysis of cached docs, no EDGAR re-fetch); signal_failed 30d retry TTL; 4MB doc truncation→extract instead of fail
- S36-04: processor defaults to haiku when ANTHROPIC_API_KEY set; remove ENABLE_LLM_ANALYSIS gate — real signal classification now automatic

- feat(watchlist): S91-01 enable 16 tickers — fix COIN/SOFI/HOOD/GE/DIS CIKs, enable UNH/MRK/TGT/LOW/CAT/RTX/T/VZ/CMCSA/NEE/COP via SEC company_tickers.json


## 2026-06-21
- test: S61-01 chart package tests (Yahoo parse + SVG render); 7 tests (Apple #2393)
- test: S60-01 store mysqlToSQLite+splitStatements tests; 20 cases (Apple #2391)

- test: S57-01 issuerregistry test suite (Apple #2385)


## 2026-06-20
- feat: S44-05 git pull --rebase before push in obs-watcher (Apple #1878)

- feat: S42-05 HaikuProvider + ENABLE_LLM_ANALYSIS feature flag — claude-haiku-4-5-20251001 analysis (Apple #1874)


## 2026-06-18

- feat(graph-seeder): S37-05 cmd/graph-seeder — seeds SQLite governance_signals from entity-graph signals.ndjson; 487 real signals inserted; dedup by filing_id
- fix(newssite): S37-01 /ask 500 — add Symbols []string to AskLandingData; serveAskLanding now populates ticker datalist from h.symbols()
- fix(newssite): S37-02 HEAD 405 — headToGet middleware in cmd/newssite wraps handler; HEAD on all routes now returns 200 with headers only
- fix(newssite): S37-03 /ticker/{sym}/feed.xml already implemented via serveTickerRSS + RenderTickerRSS (confirmed present, no changes needed)

## 2026-06-17

- fix(processor): S36-01 EDGAR 429 retry — per-host 150ms throttle (~6 req/s), up to 3 retries on 429 with 30s/60s/300s backoffs; prevents 9K+/day signal_failed spam from rate-limit hammering
- fix(processor): S36-02 skip empty-URL filings — signal_failed loaded into seen set at startup; early guard in handleOne for non-http(s) URLs; stops pre-2000 filing re-fails accumulating on every restart
- fix(processor): S36-03 raise max-doc-bytes default 4MB→16MB; proxy statements and large 8-Ks no longer fail with "document too large"
- fix(guidance-watcher,dividend-watcher,buyback-watcher): S36-05 guard ticker=="" — skip PR events with no resolvable ticker; eliminates empty-ticker signals

- fix(migrate-sqlite): strip table-level COMMENT='...' options — SQLite rejected market_data_daily migration with "near '=': syntax error"; reTableComment regex now removes table-option COMMENT= (distinct from column-level COMMENT which was already handled)
- fix(form4-watcher): strip XSL stylesheet prefix from EDGAR primaryDocument path — submissions API returns "xslF345X06/form4.xml" which resolved to rendered HTML, not raw XML; causes 0 insider transactions; stripped to "form4.xml" for correct XML; raised body read limit 4MB→32MB for JPM truncation; 30s 429-retry for Form 4 XML fetches
- fix(eventstore): per-file max-sequence cache in ReadFrom — signalapi was O(N) decoding all 82K records every 2s (86% CPU for 24h); closed files with max_seq < cursor now skipped; CPU drops to ~0% in steady state
- feat(market-data): cmd/market-data-watcher — Yahoo Finance v8 chart API scraper; emits market_data_tick events to var/market-data/ eventstore; per-ticker cursor; 2y backfill on first run then 5d incremental; 600ms inter-request delay
- feat(projector): -market-store flag tails var/market-data/ and projects market_data_tick → market_data_daily; named cursors (main/market) in projector_cursors table; both governance and market loops run per poll cycle
- feat(migration): 202606170001_market_data.sql — market_data_daily table (ticker, trade_date, OHLCV, adj_close, volume); uq_ticker_date dedup; SQLite auto-translated by existing store.RunSQLiteMigrations

## 2026-06-16
- fix(store): SQLite migration translator now strips AFTER <col> — signalapi SQLite fallback was failing on migration 202606130001, returning 503 for all signal queries
- obs-watcher: 3 AGI loop fixes — batch cursor no-hyphen format reset, context-overflow recovery (stdout capture for Prompt-too-long detection), stdout capture in invokeWithRetry
- chore(backlog): curate FatBaby observations into golden backlog — promoted 2 intake items (SHANKPIT display/fullscreen→S04, web-audit newssite/signalapi→S03), removed 1 noise item; added S35 EDIS DIS hardening section (ForceState endpoint, EDIS_DIS_COLLECTOR_URL fix, per-IP delta scoring); updated S23-06 Apple TBD; EMILY BACKLOG.md pushed. Apple #592 [task-1781616390135415763]
- fix(edis-dis): 3 pre-deploy DIS bugs — nginx parser field-index shift (all 400-class lines silently dropped when $request="-"), posture window race (hostile_ratio inflated), missing hostile_ratio from /dis/health and admin panel; 13 tests added [task-1781616390102456285]
- S30: signalapi binary built (bin/signalapi); emily start --signalapi flag wired
- perf(obs-watcher): compress runReportFooter ~30% (S34-02) — same 6 steps, fewer tokens per session

- perf(obs-watcher): primeDedupWindow 4h → 8h — saves 100K–200K tokens/day on repeated prime tasks


## 2026-06-14
- feat(newssite): Emily+ rate limit bypass — cap.query.full permission (from IDUNA subscription) bypasses all Ask Emily rate limits; askVerifier interface extended with VerifyTokenPermissions; iamguard.Guard implements it; extractUserSubAndPermissions replaces extractUserSub internally
- feat(obs-watcher): --continue flag (OBSERVATION_CONTINUE=true) enables AGI loop mode; claude invocations pass --continue so each RSI cycle continues the prior session and builds persistent context across iterations

## 2026-06-13
- feat(press-releases): /v1/press-releases/{ticker} endpoint — handler reads from in-memory docindex.Index (filtered to SourceType=="press_release"), returns {identity, document_url, snippet, filing_date, persisted_at}; DocIndex wired into ServerConfig + signalapi main (Build+Tail alongside signalindex)
- feat(source-published-at): track original source document date through pipeline — processor stamps signal.RawMetadata["source_published_at"] from FilingDiscoveredEvent.FilingDate; migration 202606130001 adds source_published_at DATE NULL + index to governance_signals; projector populates it with fallback chain (RawMetadata→filing_date→signal timestamp)
- feat(signal-api-mysql): /v1/signals/{ticker} now reads from MySQL governance_signals when available — handleSignalsByTickerMySQL ordered by COALESCE(source_published_at, filing_date) DESC; GovernanceSignalRow gains source_published_at field; /v1/governance-signals updated to same ordering
- feat(sqlite-store): internal/store/ — SQLite read-model fallback for local dev without MySQL; mysqlToSQLite() regex translator handles all FatBaby MySQL DDL patterns including ALTER TABLE IF NOT EXISTS; signalapi opens var/signalapi.db + runs migrations automatically when MYSQL_URL unset
- feat(bob-agent): cmd/bob-agent/ — FatBaby database admin agent; mysql_dsn + anthropic; tools: db_status, migrate_status, migrate_run, migrate_run_one, schema_tables, schema_describe, db_row_counts, db_query, projector_status (cursor + source_published_at null count), signal_sample; POST /task Emily agent protocol; POST /tick autonomous health sweep; listens :8085
- analysis(edis-dis): reviewed full DIS stack for WordPress+nginx pre-launch; fixed critical tailFile log-loss race in EDIS cmd/dis — old code closed and re-sought to EOF every 500ms losing lines between cycles; replaced with poll-loop that keeps fd open and only reopens on inode change; removed duplicate PHP case in edis-dis plugin; wrote deployment analysis doc (EDIS/docs/dis-deployment-analysis.md) with ranked ship order, log-tail assessment, top 3 failure modes, first-24h checklist, and pre-launch fix rationale [task-7028146271343963862]
- fix(entity-graph): observer gap false positives eliminated — `detectGaps` now uses `directorsThisBatch` (director votes found in current batch) instead of `len(graph.Nodes)` (accumulated total) for the "0 proposals" and "no directors" gap conditions; batches processing non-proxy 8-Ks (earnings, officer appointments, M&A) no longer spuriously trigger `needs_attention` observations; `BuildObservation` takes new `directorsThisBatch int` parameter
- fix(newssite): ticker search auto-navigate only on exact symbol match — `input`/`change` listeners now check the typed value against the datalist options set before navigating, preventing mid-typing redirects while still auto-navigating when a datalist option is clicked or keyboard-selected

## 2026-06-12
- feat(s21-06): Ask Emily landing page — GET /ask renders product landing page with demo form, Free/$0 + Emily+/$29 pricing tiers, and email waitlist capture; POST /api/waitlist appends to var/waitlist.txt
- feat(s21-03+04): Ask Emily signal context + rate limiting — fetchTickerContext fetches governance_signals + entity doc from signalapi for ticker; injected as [FatBaby context] prefix to Emily prompt; 5 questions/day per-IP rate limit (askRateLimiter); --signalapi-url flag + SIGNALAPI_URL env
- feat(s21-01+02): Ask Emily — POST /api/ask on newssite proxies to Emily Prime /chat; chat widget in homepage sidebar (text input, ticker field, async fetch, answer display); --emily-url flag + EMILY_BASE_URL env; graceful 503 when not configured
- docs(s20-06): docs/local-dev-setup.md — MySQL + MongoDB local dev runbook (Docker, env vars, reset/reseed, docker-compose, troubleshooting)
- feat(s20-05): signalapi CQRS query endpoints — GET /v1/governance-signals (MySQL, filter by ticker/type/since/until/limit), /v1/eps/{ticker}, /v1/entities/{ticker} (MongoDB); MYSQL_URL + MONGODB_URL env activation; graceful 503 when DB not configured
- feat(s20-04): MongoDB entity writer — internal/mongowriter writes flattened EntityDocument per ticker to MongoDB `entities` collection after each entity-graph batch; --mongo-url flag on entity-graph; graceful no-op when MONGODB_URL unset; go.mongodb.org/mongo-driver added
- feat(s20-02): MySQL projector — cmd/projector tails secwatch eventstore, projects signal_generated events into governance_signals + entity_timeline tables; migrations/mysql/ with 4 SQL files; go-sql-driver/mysql dependency added; feedserver/session.go vet fix
- docs(s20): MySQL read model schema + MongoDB entity schema specs for Ask Emily queryability layer (S20-01, S20-03)
- GTM Phase 1: IP rate limiting (10 queries/day) + paywall page for free tier on newssite ticker/search/doc pages --no-apple

- GTM funnel spec written: Ask Emily Free + Emily+ subscription + Community Editorial + Merkle Query API at docs/GTM_FUNNEL.md --no-apple


All notable changes to this project are documented in this file.

## 2026-06-11 — newssite filing date sort fix + obs-watcher rate-limit resilience

**Newssite: sort by FilingDate not ingestion order**  
Historical filings were appearing as "breaking news" because `docindex.AllSummaries()` and
`ForTicker()` sorted by `PersistedAt` (ingestion timestamp). Both now sort by `FilingDate`
descending, falling back to `PersistedAt` for docs without a filing date. Same fix applied
to `ReadLatest` in `reader.go` (fallback path when docIdx is nil). Two regression tests added:
`TestRecent_SortsByFilingDateNotIngestionOrder` and `TestForTicker_SortsByFilingDate`.

**obs-watcher: retry/backoff on Claude API rate limits**  
`invokeWithRetry` wraps all three invocation sites. Captures stderr via `io.MultiWriter`,
detects rate-limit indicators (429/overloaded/rate_limit), retries up to 3 times with
30s→90s→270s back-off. Permanent failure posts a warning Apple via `emily observe`.

## 2026-06-10 — token efficiency: conditional entity-graph rules inlining + dedup process list (task-6244152307486764200)

Sixth tic-toc iteration on RSI token efficiency. Two targeted changes:

**1. Gate entity-graph rules inlining in observation-watcher prompts**

`buildEntityGraphPrompt` previously inlined `config/entity-graph-rules.json` (~320T) in every
entity-graph Claude Code dispatch, even clean-run observations where no rule edits were needed.
Now the rules file is only included when the observation has actionable gaps, parse errors, or an
explicit rule-change request (RequestForClaude mentioning "rule" or "threshold"). The same gate
applies to the batched prompt builder.

- `cmd/observation-watcher/main.go` — `buildEntityGraphPrompt` and `buildBatchedPrompt` now check
  `len(obs.Gaps) > 0 || len(obs.ParseErrors) > 0 || request mentions rule/threshold` before inlining.
- `cmd/observation-watcher/main_test.go` — added `TestBuildPromptEntityGraphSkipsRulesWhenNoGaps`
  and `TestBuildPromptEntityGraphIncludesRulesForRuleChangeRequest`.

**2. Remove duplicate `cmd/form4-watcher` from fatbaby_process_status**

The process names slice in `fatbaby_process_status` listed `cmd/form4-watcher` twice, causing a
redundant pgrep call and a duplicate entry in the JSON result returned to Emily. Removed the
duplicate — reduces noise in tick tool results and saves ~15T per tick.

**Token savings:**
- Rules gating: ~320T saved per clean entity-graph dispatch (dispatches with signals but no gaps)
- Process dedup: ~15T saved per tick (each JSON entry ~15T, 1 extra call + 1 extra result entry)
- Combined with prior cycles: cumulative ~40K T/tick (cached) + 3M–15M T/day (prime-task dedup)

**Full efficiency history (all six cycles):**
- Cycle 1 (7b7d688): system + tool definitions cached → ~35,640T/tick savings
- Cycle 2 (EMILY 57cfb90): EMILY AnthropicClient prompt caching + cr.Value truncation → ~6,300T/task
- Cycle 3 (71e73b0): extractLessons strip artifacts + batch-window 60s + symlink fix
- Cycle 4 (d84b500): conversation history caching in runToolLoop → ~2,000–4,500T/tick
- Cycle 5 (4b2f34c): prime-task dedup gate → 3M–15M tokens/day saved
- Cycle 6 (this): conditional rules inlining + dedup process list → ~320T/clean dispatch

**Remaining highest-ROI items:**
1. RSI generator/evaluator static-prefix caching in Emily Prime (~3,600T/task)
2. Pipeline.Run() turn-level caching in Emily Prime (~4,500T/session)
3. runReportFooter compression (~350T per every Claude Code dispatch, all paths)

## 2026-06-10 — token efficiency: prime-task dedup gate in observation-watcher (task-2869191005352699116)

Fifth tic-toc iteration on RSI token efficiency. Added a 4-hour dedup window to `pollPrimeTasks()`
in observation-watcher to prevent the rsi-loop.sh preset rotation from flooding Claude Code with
identical sessions.

**Root cause**: `emily prime-task --preset rsi-token-report` fires every 30 seconds in rsi-loop.sh.
Each invocation writes a new task file with a unique `task_id` (random int63), so the existing
`recentDuplicateExists()` guard in `integration.go` (which only covers triage-issued tasks) never
triggered. Each unique file was dispatched as a fresh Claude Code session (~100K–200K tokens each).

**Changes:**
- `cmd/observation-watcher/main.go` — added `primeTaskDuplicateExists()` and a
  `primeDedupWindow = 4h` gate in `pollPrimeTasks()`. Before invoking Claude, scans the tasks
  directory for other files within the 4h window that share the same `task_type + description`.
  If found, advances the cursor (marking the file as seen) but skips the Claude invocation.
- `cmd/observation-watcher/main_test.go` — added `TestPrimeTaskDuplicateExists` covering
  match, type-mismatch, desc-mismatch, and self-check cases.

**Token savings:**
- Before: up to ~120 Claude Code sessions/hour during active rsi-loop.sh runs (30s cadence)
- After: max 1 session per preset per 4h window = 4 presets × 6 windows/day = 24 sessions/day max
- Realistic saving: 20–100 avoided sessions/day × 150K tokens = 3M–15M tokens/day

**Full efficiency history (all five cycles):**
- Cycle 1 (7b7d688): system + tool definitions cached → ~35,640T/tick savings
- Cycle 2 (EMILY 57cfb90): EMILY AnthropicClient prompt caching + cr.Value truncation → ~6,300T/task
- Cycle 3 (71e73b0): extractLessons strip artifacts + batch-window 60s + symlink fix
- Cycle 4 (d84b500): conversation history caching in runToolLoop → ~2,000–4,500T/tick
- Cycle 5 (this): prime-task dedup gate → 3M–15M tokens/day saved

**Remaining highest-ROI items:**
1. RSI generator/evaluator static-prefix caching: cache task description+criteria across iterations (~3,600T/task)
2. Pipeline.Run() turn-level caching in Emily Prime: mark last tool result per turn with cache_control (~4,500T/session)

## 2026-06-10 — token efficiency: runToolLoop conversation history caching (task-2030592364394918999)

Fourth tic-toc iteration on RSI token efficiency. Implemented conversation history caching in the
FatBaby emily-agent tool loop.

**Changes:**
- `cmd/emily-agent/main.go` — `runToolLoop()` now marks the last tool-result block of each turn
  with `cache_control: {type: "ephemeral"}`. This enables the Anthropic API to cache the entire
  conversation history through the previous turn, so each subsequent request in the loop only pays
  fresh for the new turn's content (new assistant tool_call + new tool_result), not the full
  growing history. Estimated savings per tick: ~2,000–4,500T across a 5–10 turn tick.

**Efficiency analysis (full four-cycle history):**
- Cycle 1 (7b7d688): system + tool definitions cached → ~35,640T/tick savings
- Cycle 2 (EMILY 57cfb90): EMILY AnthropicClient prompt caching + cr.Value truncation → ~6,300T/task
- Cycle 3 (71e73b0): extractLessons strip artifacts + batch-window 60s + symlink fix
- Cycle 4 (this): conversation history caching in tool loop

**Remaining highest-ROI items (not yet implemented):**
1. Prime-task batching (N queued tasks → 1 claude invocation, saves N-1 invocation overheads)
2. Entity-graph rules inline skip when hash unchanged (~320T/dispatch)

## 2026-06-10 — token efficiency: extractLessons optimisation + observation-watcher batch default (task-4870555018142724568)

Third tic-toc iteration on RSI token efficiency. Implemented the next two highest-ROI improvements.

**Changes (EMILY repo):**
- `emily-agent/rsi.go` — `extractLessons()` now returns nil for single-iteration task completions
  (no multi-iteration dynamics to learn). For multi-iteration tasks, the artifact field is stripped
  from each iteration before marshaling, reducing LLM input by ~1–4 kT per iteration stored in the
  history. Estimated savings: 1,000–5,000T per completed multi-iteration task.

**Changes (PRRJECT_FATBABY):**
- `cmd/observation-watcher/main.go` — `--batch-window` default changed from 0 to 60s: multiple
  observations that accumulate between polls are now dispatched as a single Claude Code invocation
  instead of N separate runs. Each avoided invocation saves ~5,000–10,000T in fixed context overhead.
- `cmd/observation-watcher/main.go` — `pollBatched` now skips symlinks (e.g. `latest.json →
  2026-…Z.json`). Previously the symlink sorted lexicographically after any timestamped cursor and
  would be re-processed on every poll, triggering spurious Claude invocations.

**Previous cycles:**
- Cycle 1 (commit 7b7d688): FatBaby emily-agent prompt caching on tool loop (~35,640T/tick savings)
- Cycle 2 (EMILY commit 57cfb90): EMILY AnthropicClient prompt caching + cr.Value truncation to ≤120
  chars (~6,300T/task + ~500T/iter savings)

## 2026-06-10 — token efficiency RSI report + EMILY AnthropicClient caching (task-2263691656595819891)

Wrote token-spend analysis across the three RSI subsystems and implemented the next-highest-ROI
improvement: Anthropic prompt caching in the EMILY RSI engine (symmetric to the FatBaby change).

**Analysis findings:**
- FatBaby emily-agent: ALREADY fixed (commit 7b7d688) — ~35,640 tokens/tick savings
- EMILY RSI AnthropicClient: sent system prompt uncached; 2 calls/iteration × 10 iters/task × ~700T system = 14,000T uncached overhead
- observation-watcher dispatches: run-report footer (~600T) is constant overhead per dispatch; rules JSON re-inlined each entity-graph call

**Changes (EMILY repo, commit 57cfb90):**
- `emily-agent/main.go` — `AnthropicClient.Complete()` now wraps non-empty system prompts in the
  content-block array form with `cache_control: {type: "ephemeral"}` and sends the
  `anthropic-beta: prompt-caching-2024-07-31` header. Saves ~90% on the ~700T system prompt
  re-sent on each of the 10 RSI generator/evaluator calls per task (~6,300T saved/task).
- `emily-agent/rsi.go` — `buildGenerationPrompt()` truncates `cr.Value` to ≤120 chars, capping
  artifact-quote bloat in multi-iteration failure reports (~500T/iteration savings).

**Prioritised remaining improvements:**
1. Enable `--batch-window 60s` by default in observation-watcher for prime-task dispatches
2. Skip rules-JSON inline in observation-watcher when rules file hash hasn't changed

## 2026-06-10 — emily-agent: Anthropic prompt caching for RSI token efficiency (task-5310742279807206135)

Added `cache_control: ephemeral` to the FatBaby emily-agent's Anthropic API requests, enabling
server-side prompt caching for the system prompt and tool definitions.

**Changes:**
- `cmd/emily-agent/main.go` — `AnthropicDefs()` marks the last tool definition with
  `cache_control: {"type": "ephemeral"}`, creating a cache breakpoint covering the entire
  system-prompt + tool-list prefix. `runToolLoop` now sends `system` as a content-block array
  (required by the caching API) and adds the `anthropic-beta: prompt-caching-2024-07-31` header.
- `cmd/emily-agent/tick_test.go` — updated `TestTickHappyPathRepliesOK` to read `system` as an
  array of content blocks matching the new wire format.

**Token savings:**
The `emilySystemPrompt` (~2 000 T) + 24 tool definitions (~2 400 T) = ~4 400 T are now cached
after the first call in any session. With up to 10 tool-loop iterations per `/tick`, 9 re-reads
cost 10 % instead of 100 % of input-token rate:
  - Without caching: 10 × 4 400 T = 44 000 T input overhead
  - With caching:     1 × 4 400 T write + 9 × 440 T reads = 8 360 T
  - Savings: ~35 640 T per tick (~81 % reduction on the cached prefix)

At $3/MTok and 12 ticks/hour this saves approximately $1.28/hour of system/tool overhead.

## 2026-06-10 — docs: MJOLNIR integration documentation (task-3146266896637121286)

Authored the EMILY-side MJOLNIR integration documentation in the EMILY repo:
- `EMILY/docs/MJOLNIR_INTEGRATION.md` — FCM sender design, Apple severity thresholds for push,
  device token resolution via IDUNA, morning briefing cron spec, codebase seams, Android
  notification channels (EMILY commit 3341a26)
- `EMILY/BACKLOG.md` Section 9 — all MJOLNIR backlog items were already completed and committed
  (push_tokens table, FCM sender, dispatch wiring, Android skeleton, APPLES MANIFEST.json,
  integration spec, morning briefing) as of 2026-06-09
- `EMILY/context/mjolnir-context.md` — architectural summary for future Emily sessions
  explaining the MJOLNIR mobile push channel and current implementation status

All files committed and pushed in EMILY repo. Run report in claude-runs/.

## 2026-06-09 — observation-watcher: --batch-window flag for RSI token efficiency

Added `--batch-window duration` flag (env: none; default 0 = disabled). When set to a
non-zero duration (e.g. `--batch-window=60s`), the watcher switches from single-observation
mode to directory-scan mode:

- Scans `var/emily-observations/*.json` for all files newer than the `.last-batch-processed`
  cursor (filename-ordered, same as prime-task cursor).
- Applies the existing gate filter (trivial observations skipped silently).
- Invokes Claude Code **once** for the entire batch of nontrivial observations with a
  consolidated prompt that includes all observations inline.
- Updates `.last-batch-processed` to the newest processed file regardless of gate result.

**Why:** During active entity-graph runs, 10–15 observations can accumulate per hour.
Each single-obs invocation pays ~8k tokens of context overhead. A batch of 10 observations
costs roughly the same overhead once — estimated 50–80% token reduction on busy days.

Single-observation mode (default) is unchanged. The two modes use different cursor files
so switching back and forth does not cause re-processing.

## 2026-06-08 — entity-graph: fix spurious director extraction from proposal descriptions (BA-style)

**Root cause**: `reDirectorRow` scans the entire Item 5.07 body, including non-director
proposal blocks. In BA-style annual meeting 8-Ks, shareholder proposal descriptions end with
two title-case words immediately before vote counts — e.g. "Shareholder Proposal Relating to
Independent Monitoring of the Human Rights Code 36,584,516 418,543,421 75,589,953 ...". The
last two title-cased words before the numbers ("Rights Code", "Political Activity",
"Advisory Vote") matched `reDirectorRow` and passed `looksLikePersonName`, generating
spurious director nodes with ≈7% approval and cascading `nomination_rejection` signals
(critical severity) that were false positives.

**Fix**: `ParseItem507` now detects the first proposal-splitter boundary and restricts the
body passed to `extractDirectorVotes` to the director-election section only. The full body
is still used for `extractProposals` and `extractAuditor`. Single-boundary detection avoids
scanning more than one match (`FindAllStringIndex(..., 1)`).

**Test added**: `TestParseItem507_BAProposalNoSpuriousDirectors` in `parser_test.go` covering
the BA 2011-style filing where "Rights Code", "Political Activity", and "Written Consent"
previously appeared as spurious director names.

**Impact**: All BA annual meeting 8-Ks (2010–2025) now extract 8–13 real directors with 0
spurious entries; proposals are unaffected. Eliminates false `nomination_rejection` signals
for entities "Rights Code", "Political Activity" etc.

## 2026-06-08 — entity-graph: fix compound-initial director name parsing (H.L. style)

**Root cause**: `reDirectorRow` (and `reDirectorRow3Col`) used `(?:\s+[A-Z]\.)*` for middle
initials, which requires each initial to be preceded by a space. Directors with compound
initials written without spaces between letters (e.g. "William H.L. Burnside" from ABBV 2025
annual meeting 8-K) were silently skipped — the regex expected " H." + " L." but saw " H.L.".

**Fix**: Changed `(?:\s+[A-Z]\.)*` to `(?:\s+[A-Z](?:\.[A-Z])*\.)*` in both `reDirectorRow`
and `reDirectorRow3Col`. The new pattern treats a compound initial unit like "H.L." (one space
before the unit, then chained letter-dot pairs) as a single optional group iteration. Single
spaced initials ("H. L." or " C.") continue to work as before.

**Test added**: `TestParseItem507_CompoundInitials` in `parser_test.go` covering the ABBV 2025
proxy meeting text with "William H.L. Burnside", verifying all 3 directors and both proposals
are extracted.

## 2026-06-08 — entity-graph: fix signal accumulation driving governance_health to 0

Three related fixes addressing the observation where all 4 tickers had
`governance_health_index` score=0 (critical) regardless of actual board quality:

**Root cause — duplicate signals accumulate across batch runs**: `signals.ndjson` is
append-only. Every entity-graph batch writes signals for all directors in the graph,
including idempotent signals whose `signal_id` is stable across runs (e.g.
`director_long_tenure_<name>_<ticker>`, `director_friction_<name>_<ticker>`). Over
multiple runs, these accumulate: a company with 6 long-tenure directors processed in
5 batches → 30 long-tenure signals in the file. `ScoreGovernanceHealth` counted all
copies within the 365-day window, applying the penalty 30× instead of 6×, driving
scores to 0 even for well-governed companies.

**Fix 1 — `DeduplicateSignals` in `graph.go`**: New exported function that keeps only
the most recently-detected signal per `signal_id`. Signals with empty `signal_id` pass
through unchanged. Called after `LoadSignals` in the entity-graph batch loop.

**Fix 2 — Reduce `director_long_tenure` penalty from 0.06 to 0.03**: Even after
deduplication, companies with 12+ long-tenure directors (e.g. Franklin Resources/BEN
with 12 long-serving directors) accrued 12 × 0.06 = 0.72 in penalties — disproportionate
for a low-confidence, low-urgency signal. Halved to 0.03. Adjusted in
`DefaultGovernanceHealthPenalties` in `signals.go`.

**Fix 3 — Gap detection for low proposal yield**: `detectGaps` previously only flagged
`proposalsProcessed == 0`. Added a secondary check: when `processed >= 2` and fewer than
50% of proxy filings yielded non-director proposals, a gap is now logged to the observation
with the yield rate — making partially-broken proposal parsing visible to the RSI loop.

- `internal/entitygraph/graph.go`: `DeduplicateSignals([]Signal) []Signal`
- `internal/entitygraph/signals.go`: `SignalDirectorLongTenure` penalty 0.06 → 0.03
- `internal/entitygraph/observer.go`: `detectGaps` accepts `processed int`; low-yield gap
- `cmd/entity-graph/main.go`: call `DeduplicateSignals` after `LoadSignals`
- `internal/entitygraph/signals_test.go`: 3 new tests for deduplication correctness

## 2026-06-08 — entity-graph: fix 0-proposals gap for 3-column and no-directors filings

Two related fixes addressing the "0 proposals parsed despite directors found" observation gap:

**Root cause 1 — 3-column director format not recognized**: AAPL annual meeting 8-Ks from
2011–2014 (and similar pre-majority-voting era filings) report director votes as three columns
("For | Authority Withheld | Broker Non-Votes") rather than the standard four columns
("For | Against | Abstain | Broker Non-Votes"). The existing `reDirectorRow` regex requires
four numbers after the director name; these filings produced 0 director matches, triggering
the `no_directors` early-exit in the pipeline — which also skipped proposal extraction.

**Root cause 2 — proposals gated behind director extraction**: The `no_directors` guard used
`continue` to skip the entire rest of the per-filing processing block, including proposal
scoring and auditor tracking. Any filing whose director section used an unrecognized format
would silently produce 0 proposals even when the proposal sections were perfectly parseable.

**Fixes**:
- `internal/entitygraph/parser.go`:
  - Added `reDirectorRow3Col` regex — same name pattern as `reDirectorRow` but captures only
    3 numbers (For, Withheld, BNV). Used as a fallback when the 4-column regex yields 0 results
    and the body contains an "Authority Withheld" column header (`reWithheldHeader`).
  - Added `extractDirectorVotes3Col` — maps Withheld → AgainstVotes, AbstainVotes=0,
    ApprovalPct = For/(For+Withheld), preserving historical ISS-compatible approval scoring.
  - Refactored `extractDirectorVotes` into primary (4-col) + fallback (3-col) pass.
- `cmd/entity-graph/main.go`:
  - Moved proposal scoring, `totalProposals` counting, and auditor tracking to before the
    `no_directors` guard. Proposals and auditor signals are now always emitted for any filing
    that contains a parseable Item 5.07 section, regardless of director vote format.
- `internal/entitygraph/parser_test.go`:
  - Added `TestParseItem507_ThreeColumnDirectorFormat` — verifies AAPL 2011 format produces
    correct director extraction and >= 2 proposals.

## 2026-06-08 — entity-graph: enforce HighTrustMinFilings to reduce high_trust_director noise

Implemented the previously-defined but unenforced `high_trust_min_filings` rule. The config field
existed and was documented as "relaxed for Phase 1 bootstrap" but the scoring code never checked it
— every director with ≥95% approval in any single filing received a `high_trust_director` signal
regardless of filing history depth.

**Root cause**: `scoreOneDirector` checked only `ApprovalPct >= HighTrustMinApproval`; the
`HighTrustMinFilings` field was a dead config key. In a 2-filing batch with 75 directors this
produced 19 high_trust signals (83% of all signals), masking actionable adverse signals.

**Fix**:
- `internal/entitygraph/signals.go`: added `FilterHighTrustByMinFilings(sigs []Signal, g *Graph, ticker string, minFilings int) []Signal`.
  Filters `high_trust_director` signals for directors whose per-ticker filing count in the graph
  is below the threshold. Count is ticker-scoped — a cross-board director's filings at other
  companies do not count toward the threshold at this ticker.
- `cmd/entity-graph/main.go`: calls `FilterHighTrustByMinFilings` immediately after
  `ScoreDirectorVotes` so the filter runs inside the per-filing loop before signals accumulate.
- `config/entity-graph-rules.json`: raised `high_trust_min_filings` from 1 to 2.
  Directors now require at least 2 consecutive proxy season appearances at the same ticker before
  receiving a high_trust signal — consistent with what ISS/Glass Lewis define as "track record".

Five new tests added to `internal/entitygraph/signals_test.go`:
- `TestFilterHighTrustByMinFilings_SuppressesFirstFiling`
- `TestFilterHighTrustByMinFilings_AllowsReturnDirector`
- `TestFilterHighTrustByMinFilings_TickerScopedCount`
- `TestFilterHighTrustByMinFilings_PreservesNonHighTrustSignals`
- `TestFilterHighTrustByMinFilings_NoOpWhenMinFilingsOne`

## 2026-06-08 — entity-graph: fix spurious director extraction from SCHW bare-number proposal lines

Fixed two parser defects affecting SCHW 2022 and 2023 annual meeting 8-Ks:

**Spurious directors from proposal-title nouns** (SCHW 2022 "Incentive Plan", SCHW 2023 "Equity Disclosure"):
- Added `"plan"`, `"disclosure"`, `"proposal"`, and `"policy"` to `nonNameWords` so Title-case proposal
  noun phrases at the tail of a bare-number proposal line are no longer extracted as director names by
  `reDirectorRow`. Fixes `LoadNodesFromDir` also dropping these via `isSpuriousName` retrospectively.

**Outstanding shares not parsed for SCHW** ("shares of CSC voting common stock outstanding"):
- Extended `reOutstandingShares` from `shares\s+of\s+(?:common\s+)?stock` to allow 0–4 arbitrary words
  between "of" and "stock" (`(?:\w+\s+){0,4}`), covering SCHW's "CSC voting common stock" phrasing.

Two new tests added (`TestParseItem507_SCHW2022Format`, `TestParseItem507_SCHW2023Format`) and
`TestLooksLikePersonName_HeaderRejection` updated to include the new rejected nouns.

## 2026-06-08 — entity-graph: prose-fallback parser for AMZN-style proposals (2010–2015)

Added `reProseSplitter` regex and `extractProseProposals` fallback to `internal/entitygraph/parser.go`,
closing the "0 proposals parsed despite directors found" gap for AMZN annual meeting 8-Ks from 2010–2015
that use no numbered proposal headers.

**Root cause**: AMZN filings (2010–2015) describe each non-director proposal as a full English sentence
("The appointment of Ernst & Young LLP as our independent auditor was ratified by the vote set forth below:")
with no "Proposal N", "(N)", or "N." prefix. `reProposalSplitter` correctly returned zero splits,
but `extractProposals` then returned an empty slice rather than trying an alternative strategy.

**Fix**:
- `reProseSplitter`: new regex matching AMZN prose-format proposal starters:
  `The appointment of <Firm>`, `A/An shareholder/stockholder proposal`, `An advisory vote`,
  `The compensation of our named`, `The material terms of the`.
- `extractProposals`: calls `extractProseProposals` as a fallback when primary returns 0 results.
- `extractAuditor`: refactored into `auditorFromSplits` helper; falls back to `reProseSplitter`
  when `reProposalSplitter` finds nothing, so auditor name is correctly extracted from AMZN filings.
- **Isolation guarantee**: the prose splitter is NEVER called when the primary finds proposals,
  preventing spurious sub-splits in SCHW/ABBV/BA/BLK/LLY filings that contain phrases like
  "A stockholder proposal" inside already-numbered proposal blocks.
- 3 new tests: `TestParseItem507_AMZNProseFormat`, `_AMZNProseFormat2011`,
  `_NumberedFormatUnaffectedByProseFallback`.

## 2026-06-08 — entity-graph: proposal-splitter handles BLK/SCHW/LLY filing formats

Three new alternatives added to `reProposalSplitter` and `reAuditorName` extended, fixing
the "0 proposals parsed despite directors found" gap reported in Emily observations:

- **SCHW 2021–2023 bare-number format**: "2 Ratification" (no period) — `\b(?:[2-9]|1\d)\s+[A-Z][a-z]{3,}`.
  Requires a title-case word of ≥4 chars to avoid matching vote-table headers like "For".
- **BLK 2024 Item-entity format**: "Item&#8201;2 &#8211;" (HTML thin-space entities) — `(?i:Item)(?:\s|&#\d+;)+(?:[2-9]|1\d)(?:\s|&#\d+;)`.
  Requires space/entity (not ".") after the number so "Item 5.07" is not matched.
- **LLY 2022–2026 letter format**: " b) By..." (lowercase letter a–z without opening paren) — `\s[b-z]\)\s+[A-Z]`.
  Excludes "(b)" SCHW director sub-items by requiring whitespace (not paren) before the letter.
- **`reAuditorName`**: Added `selection` and `retention` alongside `appointment` so
  SCHW-style "selection of Deloitte & Touche LLP as independent auditors" is captured.
- All `[A-Z]`/`[a-z]`/`[b-z]` character classes are intentionally outside any `(?i)` scope
  to remain case-sensitive; this prevents "of the following 16 nominees" from being mistaken
  for a proposal boundary (a false-positive observed in BLK filing).
- 3 new test cases added: `TestParseItem507_SCHWBareNumberFormat`, `_BLKItemFormat`, `_LLYLetterFormat`.

## 2026-06-08 — entity-graph: fix ratification detection and "did not approve" parsing

Two targeted improvements to `internal/entitygraph/parser.go` discovered by testing against
the real AbbVie 2026 Annual Meeting 8-K (seq=62741):

- **`reRatificationProposal`**: Added `ratified` (past-tense) to the alternation. Filings like
  ABBV-2026 phrase the vote as "The stockholders ratified the appointment of..." which contains
  neither "ratification" nor "ratifying". Previously `extractAuditor()` fell through to the
  `independent\s+registered\s+public\s+accounting` branch; now the past-tense form is an
  explicit match.
- **`reDidNotPass`**: Added `did\s+not\s+approve` phrase. ABBV-2026 proposal (4) says
  "The stockholders did not approve the management proposal regarding amendment of the
  certificate of incorporation to eliminate supermajority voting" with a 98.7% for-vote —
  an implicit supermajority requirement that isn't stated numerically. The parser was marking
  this as `Passed=true`; it now correctly sets `Passed=false`.

New test: `TestParseItem507_ABBV2026` runs against the real filing fixture at
`fixtures/entitygraph/abbv_8k_5_07_2026.txt`.

## 2026-06-08 — entity-graph: fix proposal-splitter to match real EDGAR filing formats

`extractProposals()` and `extractAuditor()` in `internal/entitygraph/parser.go` only
recognised `Proposal N` format, but actual EDGAR Item 5.07 filings use two other formats:

- **Parenthesised**: `(2) The stockholders ratified...` (common in ABBV, pharma/biotech)
- **Bare-number**: `2. Advisory Vote on...` (common in BA, industrial/aerospace)

The `Proposal N` regex never fired in production, yielding 0 proposals despite 36 directors
being found. Fixed by promoting the inline `proposalSplitter` regex to a package-level
`reProposalSplitter` that handles all three formats. Two new parser tests added:
`TestParseItem507_ParenthesisedProposals` and `TestParseItem507_BareNumberProposals`.

## 2026-06-07 — emily.cli prime-task command: operator→Emily Prime→FatBaby directed loop

`emily prime-task` (emily.cli v0.5.0) closes the operator-directed task loop. The operator
types a task description at the CLI; the command writes a structured JSON task file to
`EMILY/signals/tasks/`; the observation-watcher (`cmd/observation-watcher`) picks it up
within 10 seconds and invokes Claude Code on FatBaby with the task as its prompt.

This task (task-3623149323882848438) was the first task filed via that loop — it arrived
through the `emily prime-task` command and was acted on here.

**What exists in the FatBaby codebase:**
- `cmd/observation-watcher/main.go` — `--prime-tasks` flag and `pollPrimeTasks` function
  already poll `EMILY/signals/tasks/` alongside the Emily observation channel.
- Auto-detection: if `--prime-tasks` is not set, the watcher looks for the sibling
  `../EMILY/signals/tasks/` directory automatically.
- `buildPrimeTaskPrompt` constructs the Claude prompt from the task JSON fields
  (`description`, `context`, `acceptance_criteria`, `priority`).

**No FatBaby source changes required** — the loop was already wired. This entry documents
that the operator end (emily.cli) is now live and the first end-to-end dispatch worked.

## 2026-06-07 — RSI feedback loop: accuracy-calibrated governance health scoring

Closes the recursive self-improvement loop: historical signal accuracy records now feed
back into the composite governance health score, so signals with poor empirical precision
contribute proportionally less weight.

**`DefaultGovernanceHealthPenalties()` (`internal/entitygraph/signals.go`)**  
Extracts the previously inline penalty map into a named function. Enables callers to
retrieve, modify, and pass a custom map — foundation for RSI calibration.

**`ScoreGovernanceHealthWithPenalties()` (`internal/entitygraph/signals.go`)**  
Parameterised form of `ScoreGovernanceHealth` that accepts an explicit penalty map.
`ScoreGovernanceHealth` now delegates to this function with nil → default penalties,
preserving full backward compatibility. Upstream callers can pass accuracy-calibrated
weights to make the composite score reflect empirical signal quality.

**`AccuracyAdjustedPenalties()` (`internal/entitygraph/accuracy.go`)**  
Takes a base penalty map and a slice of `AccuracyReport`s; returns a calibrated copy
where weights are scaled by empirical precision for signal types with ≥ `minResolved`
resolved predictions. Scaling: precision ≥ 0.60 → 1.0×; ≥ 0.40 → 0.75×; < 0.40 → 0.50×.
The 0.50 floor preserves contribution for signals whose low precision may reflect long
prediction lags rather than genuine noise.

**`MinResolvedForCalibration` rule field (`internal/entitygraph/rules.go`)**  
New config field (default 5) controlling how many resolved predictions are required before
a signal type's penalty is adjusted. Added to `config/entity-graph-rules.json`.

**RSI wiring in `cmd/entity-graph/main.go`**  
At batch start, loads `accuracy.ndjson` → builds `AccuracyReport`s → calls
`AccuracyAdjustedPenalties`. Health scoring step now uses `ScoreGovernanceHealthWithPenalties`
with calibrated weights. Log line: `rsi_calibration loaded accuracy_records=N reports=M
calibrated_penalties=K`.

**Improved observation gap detection (`internal/entitygraph/observer.go`)**  
`detectGaps` and `buildRefinementRequest` now accept accuracy reports. When signal types
have < 40% precision with ≥ 5 resolved predictions, gaps are flagged for Claude recalibration
with the exact signal name, precision %, and resolved count. The `RequestForClaude` field
now includes a `RSI RECALIBRATION NEEDED` section listing under-performing signals.
`config/entity-graph-rules.json` is cited as the target for threshold changes.

**Tests**: 12 new tests across `signals_test.go` covering `DefaultGovernanceHealthPenalties`,
`ScoreGovernanceHealthWithPenalties` (nil fallback, reduced penalty raises score, zero
penalty), and `AccuracyAdjustedPenalties` (high/medium/low precision, insufficient resolved,
unknown signal type, default minResolved, immutability, integration). All 37 test packages pass.

## 2026-06-06 (22) — governance_health_index + director_long_tenure correlators — FULL COVERAGE

Two new accuracy correlation functions in `accuracy.go`:

`CorrelateGovernanceHealthIndexStability`: for each `governance_health_index` signal,
checks whether `governance_improving` or `buyback_authorization` follows at the same
ticker within ValidThrough. A composite governance health score is a forward indicator
of board quality; a subsequent governance upgrade or capital return confirms the index
was tracking real structural change. EvidenceType: "governance_improving_or_buyback_authorization". 4 tests.

`CorrelateDirectorLongTenureEntrenchment`: for each `director_long_tenure` signal, checks
whether `governance_entrenchment` or `compensation_concern` follows at the same ticker
within ValidThrough. Long-tenured directors are a structural entrenchment risk; a
subsequent flag or pay-practice concern confirms tenure has translated into governance
drag. EvidenceType: "governance_entrenchment_or_compensation_concern". 4 tests.

Both wired into entity-graph batch. Log line updated with gov_health + long_tenure counters.
All 37 test packages pass.

ACCURACY CORRELATOR COVERAGE: 31 of 31 signal types. ALL SIGNAL TYPES NOW COVERED.

## 2026-06-06 (21) — director_link + governance_peer_underperformer correlators

Two new accuracy correlation functions in `accuracy.go`:

`CorrelateDirectorLinkContagion`: for each `director_link` signal, checks whether
`director_friction` or `abstention_spike` follows at the same ticker within ValidThrough.
A shared-director link between a company under governance stress and the target is a
contagion vector; friction or abstention at the linked company confirms stress propagated
through the shared seat. EvidenceType: "director_friction_or_abstention_spike". 4 tests.

`CorrelateGovernancePeerUnderperformerDeterioration`: for each `governance_peer_underperformer`
signal, checks whether `governance_deteriorating` or `board_decay_concern` follows at the
same ticker within ValidThrough. Peer underperformance is a leading, not lagging, indicator;
subsequent governance decline confirms. EvidenceType: "governance_deteriorating_or_board_decay_concern". 4 tests.

Both wired into entity-graph batch. Log line updated with dir_link + peer_under counters.
All 37 test packages pass. Accuracy correlator coverage: 29 of 31 signal types.
Two remaining: governance_health_index, director_long_tenure.

## 2026-06-06 (20) — high_trust_director + family_control correlators

Two new accuracy correlation functions in `accuracy.go`:

`CorrelateHighTrustDirectorStability`: for each `high_trust_director` signal, checks
whether `governance_improving` or `buyback_authorization` follows at the same ticker
within ValidThrough. A high-trust director appointment signals board quality improvement;
a subsequent governance upgrade or capital return confirms the appointment translated into
durable posture change. EvidenceType: "governance_improving_or_buyback_authorization". 4 tests.

`CorrelateFamilyControlEntrenchment`: for each `family_control` signal, checks whether
`governance_entrenchment` or `compensation_concern` follows at the same ticker within
ValidThrough. Family control of a public company is a structural predictor of entrenchment;
a subsequent entrenchment flag or pay-practice concern confirms governance drag.
EvidenceType: "governance_entrenchment_or_compensation_concern". 4 tests.

Both wired into entity-graph batch. Log line updated with hi_trust + fam_ctrl counters.
All 37 test packages pass. Accuracy correlator coverage: 27 of 31 signal types.

## 2026-06-06 (19) — compensation concern + nomination rejection correlators

Two new accuracy correlation functions in `accuracy.go`:

`CorrelateCompensationConcernEscalation`: for each `compensation_concern` signal, checks
whether `abstention_spike` or `nomination_rejection` follows at the same ticker within
ValidThrough. A pay-practice objection from ISS/Glass Lewis is a leading governance signal;
shareholder pushback at the next vote confirms materiality.
EvidenceType: "abstention_spike_or_nomination_rejection". 4 tests.

`CorrelateNominationRejectionFriction`: for each `nomination_rejection` signal, checks
whether `director_friction` or `abstention_spike` follows at the same ticker within
ValidThrough. A failed nomination is often the first visible symptom of structural board
friction; subsequent friction or abstention confirms it was not isolated.
EvidenceType: "director_friction_or_abstention_spike". 4 tests.

Both wired into entity-graph batch. Log line updated with comp + nom_rej counters.
All 37 test packages pass. Accuracy correlator coverage: 25 of 31 signal types.

## 2026-06-06 (18) — special dividend + EPS filing revision correlators

Two new accuracy correlation functions in `accuracy.go`:

`CorrelateSpecialDividendCapitalReturn`: for each `special_dividend` signal, checks whether
`buyback_authorization` or `insider_buy` follows at the same ticker within ValidThrough.
A special dividend signals excess cash confidence; a subsequent capital return confirms
sustained posture. EvidenceType: "buyback_authorization_or_insider_buy". 4 tests.

`CorrelateEPSFilingRevisionDistress`: for each `eps_filing_revision` signal, checks whether
`cfo_departure`, `dividend_cut`, or `late_filing` follows at the same ticker within
ValidThrough. An EPS restatement is a leading marker of financial control breakdown;
a subsequent departure or dividend action confirms the deterioration cascade.
EvidenceType: "cfo_departure_dividend_cut_or_late_filing". 4 tests.

Both wired into entity-graph batch. Log line updated with spec_div and eps_rev counters.
All 37 test packages pass. Accuracy correlator coverage: 23 of 31 signal types.

## 2026-06-06 (17) — buyback authorization + broker nonvote anomaly correlators

Two new accuracy correlation functions in `accuracy.go`:

`CorrelateBuybackAuthorizationInsiderBuy`: for each `buyback_authorization` signal,
checks whether `insider_buy` follows at the same ticker within ValidThrough. Double
management confidence signal. EvidenceType: "insider_buy". 4 tests.

`CorrelateBrokerNonVoteAnomalyDirectorFriction`: for each `broker_nonvote_anomaly`
signal, checks whether `director_friction` follows at the same ticker within ValidThrough.
EvidenceType: "director_friction". 4 tests.

Both wired into entity-graph batch. Log line updated. All 37 test packages pass.

Accuracy correlator coverage: 21 of 31 signal types.

## 2026-06-06 (16) — abstention outlier + post-failure activist prediction correlators

Two new accuracy correlation functions in `accuracy.go`:

`CorrelateAbstentionOutlierNominationRejection`: for each `abstention_outlier` signal,
checks whether `nomination_rejection` follows at the same ticker within ValidThrough.
EvidenceType: "nomination_rejection". 4 tests.

`CorrelatePostFailureActivistPrediction`: for each `post_failure_activist_prediction`
signal, checks whether `activist_risk` follows at the same ticker within ValidThrough.
EvidenceType: "activist_risk". 4 tests.

Both wired into entity-graph batch. Log line updated. All 37 test packages pass.

Accuracy correlator coverage: 19 of 31 signal types.

## 2026-06-06 (15) — governance improving + governance entrenchment accuracy correlators

Two new accuracy correlation functions in `accuracy.go`:

`CorrelateGovernanceImprovingCapitalReturn`: for each `governance_improving` signal,
checks whether `dividend_raise` or `buyback_authorization` follows at the same ticker
within ValidThrough. Validates the composite scoring model on the positive side.
EvidenceType: "dividend_raise_or_buyback_authorization". 4 tests.

`CorrelateGovernanceEntrenchmentVoteQuality`: for each `governance_entrenchment` signal,
checks whether `compensation_concern` or `abstention_spike` follows within ValidThrough.
EvidenceType: "compensation_concern_or_abstention_spike". 4 tests.

Both wired into entity-graph batch. Log line updated. All 37 test packages pass.

Accuracy correlator coverage: 17 of 31 signal types.

## 2026-06-05 (14) — dividend raise capital cluster + governance deteriorating correlators

Two new accuracy correlation functions in `accuracy.go`:

`CorrelateDividendRaiseCapitalCluster`: for each `dividend_raise` signal, checks
whether `buyback_authorization` or `insider_buy` follows at the same ticker within
ValidThrough. Management confidence signals cluster together. EvidenceType:
"buyback_authorization_or_insider_buy". 4 tests.

`CorrelateGovernanceDeterioratingDistress`: for each `governance_deteriorating` signal,
checks whether `cfo_departure`, `director_friction`, or `late_filing` follows within
ValidThrough. Validates the composite scoring model against concrete events.
EvidenceType: "cfo_departure_director_friction_or_late_filing". 4 tests.

Both wired into entity-graph batch. Log line updated. All 37 test packages pass.

Accuracy correlator coverage: 15 of 31 signal types.

## 2026-06-05 (13) — abstention spike + board decay concern accuracy correlators

Two new accuracy correlation functions in `accuracy.go`:

`CorrelateAbstentionSpikeEscalation`: for each `abstention_spike` signal, checks
whether `nomination_rejection` or `director_friction` follows at the same ticker
within ValidThrough. EvidenceType: "nomination_rejection_or_director_friction". 4 tests.

`CorrelateBoardDecayConcernDeterioration`: for each `board_decay_concern` signal,
checks whether `director_friction`, `cfo_departure`, or `late_filing` follows within
ValidThrough. EvidenceType: "director_friction_cfo_departure_or_late_filing". 4 tests.

Both wired into entity-graph batch. Log line updated. All 37 test packages pass.

Accuracy correlator coverage: 13 of 31 signal types.

## 2026-06-05 (12) — leadership departure + buyback suspension distress correlators

Two new accuracy correlation functions in `accuracy.go`:

`CorrelateLeadershipDepartureDistress`: for each `leadership_departure` signal, checks
whether `dividend_cut`, `late_filing`, or `cfo_departure` follows at the same ticker
within ValidThrough. EvidenceType: "dividend_cut_late_filing_or_cfo_departure". 4 tests.

`CorrelateBuybackSuspensionDistress`: for each `buyback_suspension` signal, checks
whether `dividend_cut`, `late_filing`, or `cfo_departure` follows within ValidThrough.
EvidenceType: "dividend_cut_late_filing_or_cfo_departure". 4 tests.

Both wired into entity-graph batch accuracy section. Log line updated to include
lead and bb_susp record counts. All 37 test packages pass.

Accuracy correlator coverage: 11 of 31 signal types.

## 2026-06-05 (11) — dividend cut + late filing distress cascade correlators

Two new accuracy correlation functions in `accuracy.go`:

`CorrelateDividendCutDeterioration`: for each `dividend_cut` signal, checks whether
`cfo_departure`, `late_filing`, or `eps_filing_revision` follows at the same ticker
within ValidThrough. EvidenceType: "cfo_departure_late_filing_or_eps_revision". 4 tests.

`CorrelateLateFilingDistress`: for each `late_filing` signal, checks whether
`cfo_departure`, `dividend_cut`, or `eps_filing_revision` follows within ValidThrough.
EvidenceType: "cfo_departure_dividend_cut_or_eps_revision". 4 tests.

Both wired into entity-graph batch accuracy section. Log line updated to include
div_cut and late record counts. All tests pass.

Accuracy correlator coverage now spans 9 of 31 signal types: activist_risk,
director_decay, auditor_change, insider_buy, insider_sell_cluster, cfo_departure,
director_friction, dividend_cut, late_filing.

## 2026-06-05 (10) — CFO departure + director friction accuracy correlators

Two new accuracy correlation functions in `accuracy.go`:

`CorrelateCFODepartureDistress`: for each `cfo_departure` signal, checks whether
`dividend_cut`, `late_filing`, or `eps_filing_revision` follows at the same ticker
within ValidThrough. EvidenceType: "dividend_cut_late_filing_or_eps_revision". 4 tests.

`CorrelateDirectorFrictionEscalation`: for each `director_friction` signal, checks
whether `compensation_concern`, `abstention_spike`, `abstention_outlier`, or
`nomination_rejection` follows within ValidThrough. EvidenceType:
"compensation_concern_abstention_or_nomination_rejection". 6 tests.

Both wired into entity-graph batch accuracy section. Log line updated to include
cfo and dir_fric record counts. All tests pass.

## 2026-06-05 (9) — jon_signal_scan tool

New `jon_signal_scan` tool in `tools.go`: reads `var/entity-graph/signals.ndjson`,
builds composite setup score per ticker. Scoring: critical=3, high=2, medium=1, other=0.5
base; +0.5 recency bonus for signals within 30 days; combo bonuses for key pairs
(activist_risk+director_friction +4, cfo_departure+dividend_cut +5,
insider_sell_cluster+late_filing +3, compensation_concern+director_friction +3).
Signals older than 90 days excluded. Returns top N tickers sorted by score with
key signals and combo annotations. Added to jonSystemPrompt TOOLS AVAILABLE section.

## 2026-06-05 (8) — insider signal accuracy correlators (buy→capital return, sell→distress)

Two new accuracy correlation functions in `accuracy.go`:

`CorrelateInsiderBuyCapitalReturn`: for each `insider_buy` signal, checks whether
`buyback_authorization` or `dividend_raise` follows at the same ticker within ValidThrough.
EvidenceType: "buyback_or_dividend_raise". 4 tests added.

`CorrelateInsiderSellDistress`: for each `insider_sell_cluster` signal, checks whether
`dividend_cut`, `cfo_departure`, or `late_filing` follows within ValidThrough.
EvidenceType: "dividend_cut_cfo_departure_or_late_filing". 4 tests added.

Both wired into entity-graph batch accuracy section. Log line updated to include
ins_buy and ins_sell record counts alongside decay and auditor counts.

## 2026-06-05 (7) — jon_accuracy_report tool

New `jon_accuracy_report` tool in `tools.go`: reads `var/entity-graph/accuracy.ndjson`,
computes precision (confirmed / resolved), confirmed/refuted/pending counts per signal
type, returns sorted report. Allows Jon to calibrate conviction level against live
accuracy data before surfacing setups. Added to jonSystemPrompt TOOLS AVAILABLE section.
No new tests needed (pure file read + map aggregation, no business logic to unit test).

## 2026-06-05 (6) — CorrelateAuditorChangeFilingRisk accuracy tracker

New accuracy correlation function: `CorrelateAuditorChangeFilingRisk` in `accuracy.go`.
For each `auditor_change` signal, checks whether a `late_filing` or `eps_filing_revision`
signal appears for the same ticker within the auditor_change's ValidThrough window.
Confirms the restatement-risk thesis Jon uses for late_filing + auditor_change co-occurrence
setups. Wired into entity-graph batch accuracy section alongside `CorrelateDecayDeparture`.
6 tests in `accuracy_test.go`. EvidenceType: "late_filing_or_eps_revision".

## 2026-06-05 (5) — Jon agent: complete 31-signal taxonomy (4 missing types added)

Added the 4 previously undocumented signal types to Jon's SIGNAL TAXONOMY:
`compensation_concern`, `broker_nonvote_anomaly`, `abstention_spike`, `abstention_outlier`.
Added new "Vote quality / Shareholder protest signals" category. Added operating rules
for compensation_concern + director_friction co-occurrence and abstention_outlier targeting
a specific director. Taxonomy header clarified to "all 31 types."

## 2026-06-05 (4) — Jon agent full 31-signal analytical framework

### Jon Stockwell agent: complete signal taxonomy + operating rules

- **`jonSystemPrompt`**: added full `## SIGNAL TAXONOMY (31 types)` section documenting all
  signals readable via `jon_governance_signals`, grouped by category (activist/entrenchment,
  director quality, leadership/execution, capital allocation, governance health/trend, EPS).
  Each signal entry includes interpretation notes and trading context.
- **`jonSystemPrompt` OPERATING RULES**: added signal-specific guidance for 8 high-value signal
  types — activist_risk/board_decay_concern (structural IV setups), post_failure_activist_prediction
  (early-warning vol), cfo_departure (execution risk), dividend_cut (directional catalyst),
  late_filing + auditor_change (restatement risk), governance_deteriorating (trend upgrade),
  governance_peer_underperformer (relative trade), insider_sell_cluster (conviction pairing).
- **`scanPrompt`**: updated priority signal types list to include post_failure_activist_prediction,
  board_decay_concern, and governance_deteriorating alongside existing types.
- **`jon_governance_signals` tool description**: expanded from 4 signal types to full 31-type
  enumeration to accurately reflect what the tool returns.

## 2026-06-05 (3) — post_failure_activist_prediction + decay→departure accuracy tracking

### New signal: `post_failure_activist_prediction` (northstar Phase 4 item)
Per northstar: "every time a declassification vote fails, the following year has activist 13D
activity within 6 months — add a new signal: Post-Failure Activist Prediction."

- **`ScorePostFailureActivistPrediction(ticker, allSignals, windowDays)`**: fires when a
  `governance_entrenchment` signal exists within `PostFailureActivistWindowDays` (default 45)
  for the ticker. Lower-confidence early warning (0.65/0.72) with 6-month `ValidThrough` —
  distinct from `activist_risk` which requires concurrent director friction.
- Medium severity when triggering entrenchment is medium; high when high/critical.
- Added to `AllSignalTypes`, governance health penalty map (−0.12), and wired into
  `runBatch` after the activist_risk loop.
- New rule field `PostFailureActivistWindowDays` (default 45, zero-value guarded).
- 6 new tests in `signals_test.go`.

### New accuracy tracking: director_decay → leadership_departure
- **`CorrelateDecayDeparture(signals)`**: correlates `director_decay` signals with subsequent
  `leadership_departure` or `cfo_departure` signals at the same ticker. Entity matching is
  case-insensitive substring to handle canonical vs. display name variance.
- Outcome logic identical to `CorrelateActivistRisk`: confirmed / refuted / pending.
- Wired into the accuracy section of `runBatch` alongside activist risk correlation;
  decay records logged separately (`decay_departure=N`).
- 6 new tests in `accuracy_test.go`.

Signal count: 31 types. All packages pass `go test ./...`.

## 2026-06-05 (2) — Wire board_decay_concern + governance_health_trend into main loop

Both `ScoreBoardDecayConcern` and `ScoreGovernanceHealthTrend` existed in `internal/entitygraph`
but were never called from `cmd/entity-graph/main.go`. Now wired:

- **`ScoreBoardDecayConcern`**: called after `scoreDecayFromGraph` so concurrent director-decay
  signals at the same ticker produce a composite `board_decay_concern` signal.
- **`ScoreGovernanceHealthTrend`**: health history loaded at batch start from
  `var/entity-graph/health_history.ndjson`; each new health score is compared to the previous
  snapshot; delta ≥ `GovernanceHealthTrendMinDelta` (default 0.10) fires
  `governance_deteriorating` or `governance_improving`; new snapshots appended after scoring.
- New rule field `GovernanceHealthTrendMinDelta` (default 0.10, configurable in
  `entity-graph-rules.json`).
- 2 new integration tests. All 30 packages pass.

## 2026-06-05 — Director long-tenure signal + sector peer governance ranking

### `internal/entitygraph/signals.go` + `rules.go`

Two new signal types added to the entity graph RSI cycle:

**`director_long_tenure`** — fires when a director's board tenure at a single company exceeds
`LongTenureYearsThreshold` (default 12 years, configurable via `entity-graph-rules.json`).
Per ISS/Glass Lewis standards, long-tenured directors raise independence concerns; proxy advisors
increasingly vote against them. Severity: medium (12–14 years), high (15+ years). Added as a
0.06 penalty in the `governance_health_index` composite.

**`governance_peer_underperformer`** — fires when a ticker's governance health score is more than
`PeerGovernanceUnderperformThreshold` (default 0.15) below its sector median. Contextualises
absolute health scores against the peer cohort (e.g., SCHW vs. other financial sector tickers).
Severity: medium (gap 0.15–0.24), high (gap ≥ 0.25). Added as a 0.10 penalty in the health
composite. Sector comparisons require ≥2 peers with the same sector label.

### `config/watchlist.json`

All 50 entries annotated with `sector` field (GICS-style: `technology`, `financial`, `healthcare`,
`energy`, `industrial`, `consumer_staples`, `consumer_discretionary`, `communication_services`,
`utilities`). Used by `ScorePeerGovernanceRank` to group tickers for peer comparison.

### `secwatch/watchlist.go`

Added `Sector string` field to `WatchEntry` (JSON: `"sector"`, omitempty). Backwards-compatible —
existing entries without the field get empty string and are silently skipped by peer scoring.

### `cmd/entity-graph/main.go`

- Added `--watchlist` flag (default `config/watchlist.json`) to load sector mappings for peer scoring.
- `ScoreLongTenure` called after graph load (alongside `scoreDecayFromGraph`).
- `ScorePeerGovernanceRank` called after all health scores are computed for the batch.

### Tests

10 new tests: 5 for `ScoreLongTenure` (fire/no-fire/high-severity/empty-graph/custom-threshold),
5 for `ScorePeerGovernanceRank` (below-median/above-median/single-peer-skip/no-sector-skip/high-severity).
All 30 packages pass `go test ./...`.

## 2026-06-04 — Buyback watcher, NT filing watcher, eps-reconciler signals

### `cmd/buyback-watcher` + `internal/buyback`

Classifies press release bodies as share repurchase announcements:
- `authorization` / `completion` → `buyback_authorization` (low severity)
- `suspension` / `termination` → `buyback_suspension` (medium severity — cash conservation signal)
- Extracts authorized dollar amount (billion/million) and share count
- Same prwatch-body event stream + ticker map pattern as dividend-watcher
- Writes to `var/buybacks/buybacks.ndjson`; emits signals to entity-graph

### `cmd/nt-watcher`

Polls SEC EDGAR EFTS for NT 10-K and NT 10-Q (late filing notification) filings:
- Same EFTS pattern as schd13-watcher
- Classifies stated reason: `restatement`, `material_weakness`, `auditor_dispute`, `system_transition`, `additional_review`, `unknown`
- High severity for restatement/material_weakness/auditor_dispute; medium for others
- `late_filing` signal with -0.20 governance health penalty
- Writes to `var/nt-filings/filings.ndjson`; deduplicates by accession
- 180-day default lookback (NT filings are rare)

### eps-reconciler → entity-graph signals

Added `-graph-dir` flag and `scoreEPSRevision` function. When a `VerdictContradicts`
case is found, emits `eps_filing_revision` signal to the entity-graph signal store.
Signal includes extracted vs filed EPS, percentage difference, and direction (filed
higher/lower). High severity when difference ≥20%. Previously EPS contradictions were
only visible in oracle.ndjson — now they surface to Emily and Jon.

### New signal types

`late_filing`, `buyback_authorization`, `buyback_suspension`, `eps_filing_revision`
added to `AllSignalTypes` and governance health penalty map.

### Tests

26 new tests (buyback: 11, nt-watcher: 8, eps-reconciler: 0 new but now builds clean).
All passing.

## 2026-06-04 — Item 5.02 leadership parser + dividend-watcher

### Item 5.02 — Leadership departure / appointment (entitygraph/parser.go)

`ParseItem502(text string) (Item502Result, error)` extracts leadership changes from 8-K filings:
- Detects resignation, retirement, and termination via keyword patterns
- Extracts role (CEO, CFO, COO, General Counsel, Chairman, Director, etc.)
- Attempts person name extraction adjacent to the role
- Returns `ErrItem502NotFound` when Item 5.02 is absent (graceful skip)

`ScoreLeadershipChange` converts results into signals:
- `leadership_departure` — medium severity (high for involuntary terminations)
- `cfo_departure` — elevated separate signal for CFO/principal financial officer departures (high severity, 0.82 confidence); CFO departures precede restatements at elevated base rates
- Both wired into governance health penalty map (-0.10 leadership, -0.18 CFO)

**Wired into entity-graph main loop**: called for every 8-K document alongside the existing Item 5.07 path — zero new watchers required.

### `cmd/dividend-watcher` + `internal/dividend`

New dividend signal pipeline reading `pr_body_fetched` events from prwatch-body:

- `Classify(headline, body string)`: keyword classifier distinguishing:
  - `suspension` (suspend/eliminate/omit quarterly dividend) → `dividend_cut` critical severity
  - `cut` (reduce/decrease/discontinue dividend) → `dividend_cut` high severity
  - `raise` (increase/boost dividend) → `dividend_raise` low severity
  - `special` (special/extra/one-time dividend) → `special_dividend` low severity
  - `regular` (unchanged quarterly) → no signal (noise, not scored)
- `Score(ev)`: converts to entity-graph signals
- `dividend_cut` added to governance health penalty map (-0.15)
- Writes to `var/dividends/dividends.ndjson`, emits signals to entity-graph
- Ticker resolved from pr_discovered event map (same pattern as eps-processor)
- Cursor-based incremental processing; `-one-shot`, `-dry-run` flags

### New signal types

`leadership_departure`, `cfo_departure`, `dividend_cut`, `dividend_raise`, `special_dividend` added to `AllSignalTypes`.

### Tests

31 new tests (dividend: 11, parser Item 5.02: 7 + 2 scoring, entitygraph schd13: passing). All green.

## 2026-06-04 — Form 4 insider watcher + governance health trend signal

### New: `cmd/form4-watcher`

Polls SEC EDGAR submissions API for Form 4 (Statement of Changes in Beneficial Ownership) filings for every watchlisted CIK. Fetches Form 4 XML documents, parses non-derivative transactions, and emits conviction signals to the entity-graph signal store.

- Writes raw transactions to `var/form4/transactions.ndjson` (deduped by accession number)
- Respects EDGAR rate limit (200ms between document fetches)
- De-duplicates across poll cycles via accession number set
- Flags: `-watchlist`, `-graph-dir`, `-out-dir`, `-lookback`, `-one-shot`, `-dry-run`
- Wired into Emily: `fatbaby_start_process form4-watcher`, included in `fatbaby_process_status`

### New: `internal/insider` package

Form 4 XML parser and signal scorer.

- `ParseForm4XML`: parses SEC Form 4 XML (nonDerivativeTable); handles BOM, empty tables, malformed docs
- `InsiderTransaction`: typed record with role (IsOfficer/IsDirector/IsTenPct), code, shares, price, value
- `TransactionCode.IsConviction()`: true only for P (purchase) and S (sale) — awards/exercises/gifts excluded
- `ScoreInsiderActivity`: produces conviction signals from a transaction window:
  - `insider_sell_cluster`: ≥3 distinct officers/directors sold within any 30-day rolling window — high severity
  - `insider_buy`: open-market purchase by C-level officer (CEO/CFO/COO/CTO/General Counsel) or any insider ≥$100k

### New: `ScoreGovernanceHealthTrend` (entitygraph/signals.go)

Compares the current governance health index score to the previous stored score and emits a trend signal when the delta exceeds 0.10:
- `governance_deteriorating`: score dropped — medium severity (high if drop ≥0.20)
- `governance_improving`: score rose — low severity

Wired into `ScoreGovernanceHealth` penalty map: `insider_sell_cluster` penalises health by -0.12, `governance_deteriorating` by -0.08.

### New: health history persistence (entitygraph/accuracy.go)

`HealthSnapshot`, `AppendHealthSnapshot`, `LoadHealthHistory` — append-only NDJSON file at `var/entity-graph/health_history.ndjson`. Last-write-wins per ticker for trend comparison.

### New signal types

`insider_buy`, `insider_sell_cluster`, `governance_deteriorating`, `governance_improving` added to `AllSignalTypes`.

### Tests

25 new tests across `internal/insider` (10), `internal/entitygraph/signals_test.go` (6 trend tests), `internal/entitygraph/accuracy_test.go` (3 health history tests). All passing.

## 2026-06-03 — Jon Stockwell agent (cmd/jon-agent)

### Jon Stockwell — Options Strategist Agent

New agent: `go run ./cmd/jon-agent` — Jon Stockwell, options strategist for EINHORN_INDUSTRIAL.

**System prompt**: distilled from docs/jon.md. Jon applies divergence analysis across fatbaby
pipeline data to surface options trade setups. Core doctrine: protect capital, patience before
the thesis, audacity on entry, ruthlessness on exit, edge in the divergence.

**FatBaby pipeline tools** (reads live data):
- `jon_governance_signals` — entity-graph signals (director friction, board decay, activist risk, governance health)
- `jon_eps_status` — EPS oracle precision and recent extractions
- `jon_read_press_releases` — press release bodies from prwatch pipeline
- `jon_read_filings` — 8-K source documents from secwatch pipeline
- `jon_read_guidance` — forward guidance signals (raises/lowers/maintains)

**Setup audit trail**:
- `jon_log_setup` — logs structured trade setup to var/jon-setups/setups.ndjson
- `jon_read_setups` — reads logged setups with ticker/status filtering
- `jon_publish_to_prime` — surfaces high-conviction setups to Emily Prime's integration layer

**Market data stubs** (wired up once MARKET_DATA_API_KEY + MARKET_DATA_PROVIDER set):
- `jon_options_chain`, `jon_price_data`, `jon_iv_rank` — return clear "not connected" messages
- `jon_expected_move` — pure-math calculator (price × IV × sqrt(DTE/365)) — works immediately

**HTTP endpoints**: GET / (dark-themed web UI), POST /chat (interactive), POST /scan (autonomous divergence sweep)

**Wired into Emily**: jon-agent added to `fatbaby_start_process`, `fatbaby_stop_process`, and `fatbaby_process_status` tools.

**Tests**: 8 new tests — expected move math, bad input, market data stubs, setup audit log CRUD, ticker filtering, publish-to-prime.

## 2026-06-03 — test coverage: schd13-watcher, newssite earnings calendar, commentary, guidanceread

### schd13-watcher tests (0 → 6)
- Extracted `parseEFTSResponse(result, ticker, cik)` for testability (was inline in `fetchSchd13`)
- Incomplete hits (empty FormType/FileDate) are now skipped in `parseEFTSResponse`
- Tests: `padCIK` zero-padding, multi-hit parsing, empty response, missing fields guard,
  `loadWatchlist` happy path + missing file, round-trip JSON parse

### newssite earnings calendar tests (new)
- `TestEarningsCalendarSection`: wires earningscal.Store with upcoming date; verifies
  AAPL/Announced/BMO/Q3/Upcoming all appear in `/section/earnings` response
- `TestEarningsCalendarSection_NilStore`: graceful render without store wired
- `TestEarningsCalendarSection_PastDateExcluded`: past dates not shown in upcoming table
- `TestFormatPeriodStr`, `TestFormatUpcomingDate`, `TestEarningsStatusLabel`

### commentary tests (0 → 6)
- Load, newest-first ordering, limit, case-insensitive ForTicker, skip-invalid-records, empty dir

### guidanceread tests (0 → 7)
- Load, newest-first, ForTicker, skip-empty-headline, empty dir, formatPeriodStr, ArticleToView field mapping

## 2026-06-03 — entity graph RSI: board decay composite, configurable comp-vote keywords, earnings calendar API

### Entity graph RSI improvements (Emily Prime task: "improve entity graph stuff")

**New signal: `board_decay_concern`**
- `ScoreBoardDecayConcern(ticker, allSignals, r)` fires when ≥ `min_board_decay_count`
  distinct directors at a ticker have active `director_decay` signals in the trailing
  `board_decay_concern_window_days` window (default 3 directors / 730 days)
- Severity: medium at threshold, high at 2× threshold
- Deduplication: multiple decay signals for the same director count as one
- Added to `AllSignalTypes` and governance health penalty map (−0.15 per occurrence)
- 5 new tests: fires at threshold, high severity at 2×, no-fire below, dedup, stale signals

**Configurable comp-vote keywords**
- `isCompVote` now uses `r.CompVoteKeywords` instead of hardcoded patterns
- `Rules` struct: new `CompVoteKeywords []string` field (JSON: `comp_vote_keywords`)
- `DefaultRules()` adds "remuneration" and "advisory vote on pay" to the default set
- `entity-graph-rules.json`: updated with the expanded keyword list
- 2 new tests: default keywords, custom keyword override

**New Rules fields (all hot-reloadable via `config/entity-graph-rules.json`)**
- `comp_vote_keywords` — list of lowercase substrings identifying advisory comp votes
- `min_board_decay_count` — threshold for board_decay_concern (default 3)
- `board_decay_concern_window_days` — lookback window (default 730)

### signalapi: earnings calendar endpoint

- Fixed pre-existing build failure: `handleEarningsCalendar` was registered but never implemented
- `GET /v1/earnings-calendar` — returns earnings dates from `earningscal.Store`
  with optional filters: `ticker`, `from`, `to`, `status`, `upcoming=1`, `limit`
- Returns 503 when `EarningsCal` store is not configured (optional feature)

### Documentation

- README: added **Environment Variables** table covering all 20 env vars across all processes
  (addresses outstanding Emily observation 2026-05-31T204315Z)

## 2026-06-01 — guidance feed: extractor, watcher, newssite section /section/guidance

### Forward guidance pipeline

New end-to-end pipeline for company forward guidance (raises, lowers, maintains, initiates,
withdraws EPS and revenue guidance from earnings press releases).

**`internal/guidance/` (new package)**
- `types.go`: `GuidanceData`, `Article` structs; `Action` (raised/lowered/maintained/
  initiated/withdrawn/updated), `Metric` (eps/revenue/both), `Period` (Q1–Q4/FY + year)
- `extract.go`: `Extract(text, identity, ticker)` — scans press release text for guidance
  trigger words, detects action via ordered regex matching, extracts EPS ranges
  (`$X.XX to $X.XX`), revenue ranges (`$X.X billion to $Y.Y billion`), period, and
  action. Confidence-scored 0.0–1.0. Fixed sentence splitter uses `\.\s+` regex to avoid
  splitting decimal numbers.
- `article.go`: `Generate(g, now)` — produces publishable Article when confidence >= 0.60.
  Builds headline: "{Issuer} raises/lowers FY 2026 EPS guidance to $X.XX–$X.XX"
- 6 tests: EPS range, revenue range, withdrawn, no guidance, generate, low confidence reject

**`cmd/guidance-watcher/` (new command)**
- Polls `var/prwatch-body` for `pr_body_fetched` events (same as eps-processor)
- Runs `guidance.Extract` on each press release body
- Appends publishable articles to `var/guidance/articles.ndjson`
- Cursor at `var/guidance-watcher/.cursor`
- `go run ./cmd/guidance-watcher`

**`internal/newssite/guidanceread/` (new package)**
- `Store`: append-only NDJSON reader, in-memory by-ticker index, `StartRefresh`
- `ArticleToView`, `GuidanceItemView`

**Newssite**
- `GuidanceItemView` struct with ActionLabel ("Raises"), MetricLabel ("EPS & Revenue")
- `ToGuidanceItemView`, `GuidanceItemsFrom`, `RenderGuidancePage`
- `/section/guidance` route → live guidance feed (color-coded by action)
- `guidanceTemplate`: action-colored kicker (green=raised, red=lowered, amber=withdrawn)
- "Guidance" added to sectionsrail nav
- `cmd/newssite/main.go`: `-guidance-dir` flag, startup load + 60s refresh
- `CLAUDE.md`: guidance-watcher added to pipeline processes table

## 2026-06-01 — Emily commentary: newssite ingest + fatbaby_publish_commentary tool

### Emily-authored governance articles (Emily observation 20260530T215349Z)

Emily can now publish narrative governance articles directly to the newssite,
transforming it from a raw filing viewer into a live intelligence newspaper.

**New package: `internal/newssite/commentary`**
- `commentary.Article` struct: id, ticker, headline, body, preview, byline, kind,
  filing_date, published_at, signal_ids
- `commentary.Store`: append-only NDJSON store at `var/commentary/articles.ndjson`;
  in-memory by-ticker index with background refresh
- `commentary.Append()`: write path used by the HTTP ingest endpoint

**Newssite handler**
- `Handler.SetCommentaryStore(cs, dir)`: wires the store
- `POST /api/commentary`: JSON ingest endpoint — Emily POSTs a commentary article;
  stored immediately and visible on the next ticker page load
- Commentary articles prepended to `secDocs` in `serveTicker` via `commentaryToEntry()`
- `commentaryToEntry()`: maps `commentary.Article` → `DocEntry{SourceType: "emily_commentary"}`

**Rendering**
- `docKicker`: new `emily_commentary` case → kicker "Emily · Intelligence" with
  `kicker-emily` class (purple, `#7c3aed`)
- `docByline`: returns "By Emily — Signal Intelligence" for emily_commentary
- `siteCSS`: `.kicker-emily { color: #7c3aed; }`

**`cmd/newssite/main.go`**
- `-commentary-dir` flag (default `var/commentary`); loads commentary store at startup

**`cmd/emily-agent/main.go`** — new `fatbaby_publish_commentary` tool
- Writes article to `var/commentary/articles.ndjson` (append-only)
- Fields: headline, body, ticker, kind, filing_date, signal_ids
- Auto-generates id, preview, byline, published_at
- Visible immediately on newssite after next page load (30s refresh cycle)

## 2026-06-01 — newssite date fix, ticker search UX, GitHub issue creation

### Newssite: filing date as primary display date (Emily observation fix)

Historical filings were showing the indexed date (PersistedAt) in the detail page edition bar
and fact box, making 2019 filings appear as "breaking news" from today.

- **`internal/newssite/render.go`** `buildDetailPage`: `DateStr` now shows the SEC filing date
  (primary) via `displayDateShort()`. New `PersistedStr` field carries the indexed timestamp.
  Added `IsHistorical` boolean (filing older than 90 days). Historical filings get the
  `ARCHIVE ·` kicker prefix on the detail page, matching the article card behavior.
- **`internal/newssite/templates.go`** detail template: edition bar shows filing date, not
  indexed date. Fact box rows renamed "Filed" / "Indexed". Historical badge added above article
  headline: "🗂 HISTORICAL FILING — Originally filed DATE. Indexed DATE."

### Newssite: ticker search auto-navigates on datalist click and Enter

- **`internal/newssite/templates.go`** masthead search script: replaced `change` event
  listener (unreliable for datalist selection across browsers) with `input` event + form
  `submit` intercept. Clicking a datalist suggestion or pressing Enter on an exact-match
  ticker now navigates directly to `/ticker/{TICKER}` without requiring a second Go button click.

### Emily agent: GitHub issue creation on observation write

- **`cmd/emily-agent/main.go`** `createGitHubIssue()`: creates a GitHub issue via the
  REST API whenever `fatbaby_write_observation` is called. Reads `GITHUB_TOKEN`,
  `GITHUB_OWNER`, `GITHUB_REPO` env vars; silently skips when not configured.
  Labels: `emily-observation` + severity-mapped label (info→enhancement, warn→bug,
  error→critical). Runs in a goroutine so it never blocks observation writes.
- `fatbaby_check_env` now reports GitHub integration status (ok/warn).
- `.env.example` updated with `GITHUB_TOKEN`, `GITHUB_OWNER`, `GITHUB_REPO`.

## 2026-06-01 — entitygraph RSI: BNV de-duplication, abstention outlier signal, spurious node cleanup

### Signal quality improvements (Emily Prime task: improve entity graph via RSI)

Three entity-graph signal quality improvements from the recursive self-improvement loop:

**BNV anomaly de-duplicated per filing**
- `internal/entitygraph/signals.go`: `broker_nonvote_anomaly` now emits once per
  filing instead of once per director. All nominees share the same broker non-vote
  count; the old code produced N identical signals per filing. New signal ID:
  `bnv_anomaly_{ticker}_{filingdate}` (no entity field — it's a filing-level signal).
- Existing signals in `var/entity-graph/signals.ndjson` with the old per-director
  IDs remain valid; they will age out naturally or can be purged during a full
  reprocessing pass.

**New `abstention_outlier` signal type**
- `ScoreAbstentionOutliers(votes, ticker, filingDate, r)`: fires when a director's
  abstain rate exceeds `r.AbstentionOutlierMultiplier` (default 2.5×) times the
  filing-median abstain rate AND exceeds 1% absolute. Detects targeted protest voting
  or proxy advisor differentiated recommendations directed at a specific director.
- `config/entity-graph-rules.json`: `abstention_outlier_multiplier: 2.5` (hot-reloaded).
- Added to `AllSignalTypes` and `GovernanceHealth` penalty table (−0.05).
- 3 new tests: fires on outlier director, suppresses uniform abstain rates, BNV fires once.

**Spurious node cleanup**
- `parser.go`: new `isSpuriousName(s)` function — lightweight check that rejects
  names containing `nonNameWords` keywords. Less strict than `looksLikePersonName`;
  safe to apply to persisted node records (which may be single-word or short names).
- `graph.go` `LoadNodesFromDir`: drops nodes whose names fail `isSpuriousName`.
  Cleans up legacy "Against Abstained" and "Broker Non-Vote" nodes that were stored
  before the parser-level fix landed without requiring a manual data migration.
- `graph.go` `CompactNodes`: same filter applied during compaction.

**Other**
- `.env.example` created at repo root (previously documented inline in README only).

## 2026-06-01 — emily-agent: IDUNA M2M token acquisition (HQ-SPEC-IAM-095 §3.1)

### Emily agent identity governance (spec §3.1)

Emily now acquires a short-lived IDUNA JWT at startup and refreshes it on tick.

- **`cmd/emily-agent/main.go`**: `acquireIDUNAToken`, `idunaTokenFresh`,
  `loadIDUNAAgentCfg` — reads `IDUNA_BASE_URL`, `IDUNA_AGENT_NAME`,
  `IDUNA_AGENT_SECRET` at startup; calls `POST /api/v1/auth/agent`; stores
  JWT in `Server.idunaToken`. `handleTick` refreshes when within 5 min of expiry.
- **`README.md`**: four IDUNA env vars documented.

## 2026-06-01 — iamguard: IDUNA IAM middleware (HQ-SPEC-IAM-095); dashboard + emily-agent protected

### IAM enforcement per integration mandate HQ-SPEC-IAM-095

Implements the full downstream IAM enforcement specification for PRRJECT-FATBABY.

- **`internal/iamguard/`** (new package): `RequirePermission(perm)` http.Handler
  middleware wrapping `internal/idunaauth`. No-op when IDUNA not configured —
  backward-compatible with unauthenticated local deployments. `NewFromEnv()`
  reads `IDUNA_JWKS_URL` env or `config/iam_config.json`. 7 tests.
- **`internal/idunaauth`**: JWKS cache TTL updated 5 min → 60 min per spec §2.1.
- **`config/iam_config.json`** (new): externalizes IDUNA JWKS URL per spec §4.
- **`internal/server/sse.go`**: dashboard `/events` protected by `fatbaby.read`.
- **`cmd/emily-agent`**: `/chat` protected by `fatbaby.operator`; `/tick` by
  `governance.admin` per spec §2.2 endpoint protection matrix.
- Spec endpoint coverage: signalapi ✅, dashboard ✅, emily-agent chat ✅, tick ✅.

## 2026-06-01 — entitygraph: GovernanceHealth FilingDate window fix; Phase 3 complete

### GovernanceHealth window correctness (RSI)

`ScoreGovernanceHealth` was filtering signals using only `DetectedAt`, so backfilled
historical signals (2019 filing date, 2026 detection date) were incorrectly included
in the current governance health window. Aligns with `ScoreCompositeActivistRisk`
which already uses the `FilingDate || DetectedAt` pattern.

- **`internal/entitygraph/signals.go`**: `ScoreGovernanceHealth` now uses
  `FilingDate` when present, falls back to `DetectedAt` when empty.
- **`internal/entitygraph/signals_test.go`**: Two new tests —
  `TestScoreGovernanceHealth_FilingDateWindow` and `_FilingDateFallback`.
- **`README.md`**: Phase 3 status updated to ✅ (50-ticker watchlist, director
  centrality, `director_link`, IDUNA JWT auth). `IDUNA_JWKS_URL` added to env
  vars table.

### Emily agent: richer check_env + read_governance_signals tool

- **`fatbaby_check_env`**: Now validates ANTHROPIC_API_KEY, claude CLI path
  (via PATH and `~/.local/bin` fallback), and observation-watcher cursor state.
- **`fatbaby_read_governance_signals`** (new): Reads `var/entity-graph/signals.ndjson`
  directly; returns ticker summaries (signal count, severity breakdown, latest filing
  date) plus high/critical signals. Supports ticker filter and result limit. Emily
  can now inspect governance intelligence without running a full entity-graph batch.

## 2026-06-01 — signalapi: IDUNA JWT auth (FARTHQ ecosystem integration)

### IDUNA JWT verification for signal API

Wires IDUNA — the FARTHQ central trust authority — into the signal API so
FARTHQ consumers can authenticate with IDUNA-issued JWTs instead of (or
in addition to) static API keys.

- **`internal/idunaauth/`** (new package): Stdlib-only ES256 JWT verifier
  backed by IDUNA's JWKS endpoint (`/api/v1/jwks`). Fetches keys at startup,
  caches with 5-minute TTL, re-fetches on unknown `kid`. Accepts Bearer JWTs
  with `fatbaby.read` permission. No external dependencies.
- **`internal/apiserver/server.go`**: `ServerConfig.IDUNAVerifier` field added.
  `checkAuth()` accepts static API keys **or** IDUNA JWT (both paths active).
- **`cmd/signalapi/main.go`**: Reads `IDUNA_JWKS_URL` at startup; wires
  verifier when set, falls back gracefully on JWKS failure.
- 8 tests: valid token, expired, wrong key, unknown kid, HasPermission,
  base64url padding, EC key round-trip, TTL-triggered re-fetch.

## 2026-06-01 — entitygraph: fix spurious header-node extraction (Against Abstained, Broker Non-Vote)

### Parser correctness fix (RSI observation)

Vote-table column headers and aggregate-row labels (e.g. "Against Abstained",
"Broker Non-Vote") were being extracted as director names because they pass the
title-case heuristics but were absent from the `headerPhrases` blocklist.

- **`internal/entitygraph/parser.go`**: Two-layer defence: (1) expanded
  `headerPhrases` with missing variants; (2) new `nonNameWords` set that rejects
  any candidate containing `against`, `abstain`, `abstained`, `withheld`,
  `non-vote`, `broker`, or `cast` regardless of casing.
- **`internal/entitygraph/parser_test.go`**: Three new tests —
  `TestLooksLikePersonName_HeaderRejection`, `TestLooksLikePersonName_ValidNames`,
  `TestParseItem507_NoSpuriousHeaderNodes`.

## 2026-06-01 — observation-watcher: claude CLI PATH fix; watchlist expanded to 50 tickers

### observation-watcher: automatic claude CLI resolution (Emily observation fix)

Resolves the broken autonomous feedback loop reported by Emily (2026-05-31):
the `claude` CLI was installed at `~/.local/bin/claude` but not on the process PATH,
causing every observation dispatch to fail silently.

- **`cmd/observation-watcher/main.go`**: Added `resolveCmd()` helper. When the
  command is not found via `exec.LookPath`, it falls back to `~/.local/bin/<cmd>`,
  `~/bin/<cmd>`, and `/usr/local/bin/<cmd>` in order. The resolved path is applied
  at startup and logged, so PATH issues are visible immediately rather than at
  first dispatch.

### Watchlist expanded to 50 tickers (Emily observation)

Per Emily's observation on O(1) watchlist design and sector diversity requirements.

- **`config/watchlist.json`**: 24 → 50 entries. Added megacap tech (AMZN, META,
  TSLA, NFLX), pharma (JNJ, PFE, ABBV), energy (XOM, CVX), retail (WMT, HD, COST),
  and aerospace (BA) as enabled entries. Added 13 further tickers (UNH, MRK, TGT,
  LOW, CAT, RTX, GE, T, VZ, CMCSA, DIS, NEE, COP) as disabled pending EDGAR CIK
  verification. Northstar Week-8 target "50+ companies tracked" is now met.

## 2026-06-01 — Entity graph RSI: acceleration decay, critical severity tiers, activist risk escalation

### Entity graph signal improvements (recursive self-improvement cycle)

Implements Emily Prime's directive to improve entity graph signal quality using RSI.

- **Acceleration decay**: `ScoreDirectorDecay` now fires on a single sharp year-over-year
  drop exceeding `acceleration_decay_pp_threshold` (default 4.0 pp), even before
  `decay_min_years` data points are available. Catches the Herringer pattern early.
  New severity tier: >5 pp avg drop → high (was medium); single-year acceleration → medium.

- **Critical governance health**: `ScoreGovernanceHealth` now emits `critical` severity
  when composite score falls below 0.20 (multiple concurrent governance failures).
  Previously topped out at `high`.

- **Activist risk escalation**: `ScoreCompositeActivistRisk` escalates to `critical`
  when a `nomination_rejection` co-occurs with `governance_entrenchment` (base rate
  ~75% for activist 13D within 6 months vs ~60% for plain friction). Also now accepts
  signals by `FilingDate` in addition to `DetectedAt` so stale processing timestamps
  don't exclude recent filings from the window.

- **`config/entity-graph-rules.json`**: Added `acceleration_decay_pp_threshold: 4.0`.
  Hot-reloaded; no process restart required.

- **6 new tests** covering all new severity paths and the FilingDate window fix.

## 2026-05-31 — Emily Prime integration: auto-commit to Prime, observation-watcher task polling

### Emily Prime ↔ FatBaby feedback loop wired

Implements the integration layer between FatBaby-Emily and Emily Prime as specified
in `EMILY/emily-prime-spec.md`. The loop is now closed: FatBaby observations flow
up to Emily Prime automatically; Emily Prime's directed tasks flow back down to
the observation-watcher without manual configuration.

- **`cmd/emily-agent/main.go`**: `fatbaby_write_observation` now auto-commits to
  Emily Prime's `signals/observations/` directory (and git-commits to the EMILY
  repo) whenever severity is `error`, `warn`, `critical`, or `high`. No separate
  `fatbaby_commit_to_prime` call needed for the common escalation path. Tick prompt
  updated to clarify severity semantics and mention the auto-forwarding behaviour.

- **`cmd/observation-watcher/main.go`**: Added auto-detection of Emily Prime's
  tasks directory. If `--prime-tasks` / `EMILY_PRIME_TASKS_DIR` is not set, the
  watcher checks for `../EMILY/signals/tasks` relative to `--root`. In the standard
  sibling layout (`~/PRRJECT_FATBABY` + `~/EMILY`) this fires automatically with
  zero configuration.

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
