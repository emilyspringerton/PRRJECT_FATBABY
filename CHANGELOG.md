# Changelog

All notable changes to this project are documented in this file.

## 2026-05-28
- Created the canonical `./var/` directory skeleton (`secwatch`, `prwatch`, `prwatch-body`, `logs`, `emily-observations`) so pipeline processes can write to the paths CLAUDE.md and `cmd/emily-agent` already expect.
- Added `.gitignore` covering runtime data (`var/`, `data/`, `bar.bak/`, stray `cmd/*/var/`) so pipeline runs no longer pollute `git status`.
- Added `fatbaby_write_observation` and `fatbaby_read_observation` tools to `cmd/emily-agent` so Emily can publish structured findings to `var/emily-observations/latest.json` — the handoff file for the Emily ↔ Claude Code feedback loop. Extended Emily's system prompt to describe when to use them.

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
