# Claude Code Run — RSI Signal Quality + Emily Prime Backlog Sync
**Date:** 2026-06-08T22:00:00Z
**Triggered by:** Emily Springerton (manual: recursive self-improvement + golden backlog)

## Observations analyzed
Reviewed all 2026-06-08 FatBaby entity-graph observations (15+ runs). Key patterns:
- `director_long_tenure` generating 8–63 signals/run, including spurious names
- 0 proposals in most runs (BA-style proposal-splitter miss)
- `governance_health_index` always 0 (critical) for all tickers
- `director_link` always 0 across all runs
- All accuracy_scores precision=0 (feedback loop not closing)
- Human observations: "eo" alias, APPLES git auto-sync

## Changes made

### PRRJECT_FATBABY — fix: block proposal-topic entities from director_long_tenure
- Extended `nonNameWords` with 13 proposal-topic terms (compensation, consent, meetings,
  activity, contributions, benefits, executives, chairman, retirement, association, political,
  code, rights). Prevents new spurious names from being parsed as directors when the
  proposal-splitter misses the boundary.
- Added `isSpuriousName()` guard in `ScoreLongTenure()` — skips existing graph store nodes
  whose names contain non-person words (e.g., "Rights Code" from BA 2011 8-K).
- Extended `TestLooksLikePersonName_HeaderRejection` with 9 observed spurious names.
- `go test ./...` passes. Commit: c115444

### EMILY — rsi: preset rotation + FatBaby combined tick
- `rsi-loop.sh`: Added PRESET_LIST env var (default: rsi-token-report entity-graph-refinement
  eps-coverage-review). Each iteration cycles to the next preset (iter mod count), giving
  broader RSI surface coverage.
- `rsi-loop.sh`: Added FatBaby combined tick phase after TOCK. POSTs to
  FATBABY_AGENT_URL/tick (default: http://localhost:8080/tick). Skip with SKIP_FATBABY_TICK=1.
- Commit: f43dfec

### emily.cli — feat: eo alias for emily observe
- `emily eo` now routes to RunObserve (same as `emily observe`). Human observation request.
- Binary reinstalled to ~/.local/bin/emily. Commit: 1003e0e

### EMILY/BACKLOG.md — golden backlog synced from observations
- Marked rsi preset rotation, FatBaby tick, eo alias, director_long_tenure fix complete
- Added Section 7: SIGNAL QUALITY (5 open items from June 8 observation analysis)
  - extractProposals() regex miss for BA-format filings (explicit gap in observation)
  - governance_health_index always 0
  - signal accuracy precision=0 (feedback loop not closing)
  - director_link always 0
  - APPLES dedicated git repo auto-sync (human observation)

## Tests
- PRRJECT_FATBABY: `go test ./...` all green
- emily.cli: `go test ./...` all green, `emily eo --dry-run` verified

## Next backlog items (Section 7 open)
1. extractProposals() regex — needs actual failing BA 8-K filing text as fixture
2. governance_health_index calibration investigation
3. signal accuracy feedback loop mechanism
4. director_link investigation (BuildEdgesFromFiling + ScoreDirectorLinks)
5. APPLES git repo auto-sync spec + implementation
