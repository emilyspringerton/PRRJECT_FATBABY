# Changelog

All notable changes to this project are documented in this file.

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
