# Add `entity-graph` to the Process Management Tool — ✅ Completed

> **Status**: Done. `entity-graph` is registered in `cmd/emily-agent/main.go` alongside all other
> pipeline processes. `fatbaby_start_process("entity-graph")`, `fatbaby_process_status`, and
> `fatbaby_read_log("entity-graph")` all work. Emily also has a dedicated
> `fatbaby_run_entity_graph_once` tool for synchronous one-shot runs.

## Problem (historical)

The MCP tool that starts pipeline processes (`fatbaby_start_process`) supports six named processes:
`secwatch`, `processor`, `newssite`, `dashboard`, `prwatch`, `prwatch-body`

`entity-graph` is not in this list, so it cannot be started via the assistant — users have to run it manually in a terminal.

## Goal

Add `entity-graph` as a valid, startable process so calling `fatbaby_start_process` with `"entity-graph"` launches `go run ./cmd/entity-graph` (or its compiled binary equivalent) in the background, the same way other processes are managed.

## Tasks

1. **Find the process registry** — locate where the six supported process names are defined (likely a map, switch statement, or config struct in the MCP server or tool handler code).

2. **Add `entity-graph`** to that registry with:
   - The correct command: `go run ./cmd/entity-graph` (or the compiled binary path if a build step exists)
   - Working directory: same root as the other processes
   - Any stdout/stderr log routing consistent with how other processes handle logs (e.g., writing to `var/logs/entity-graph.log`)

3. **Add a status check** — ensure `fatbaby_process_status` (or equivalent) can report whether `entity-graph` is running or stopped, consistent with how other processes are reported.

4. **Verify restart safety** — if `entity-graph` is already running, starting it again should either no-op or cleanly replace the existing process, not spawn a duplicate.

## Acceptance Criteria

- `fatbaby_start_process("entity-graph")` starts the process without error
- `fatbaby_process_status` includes `entity-graph` in its output with a correct running/stopped state
- Logs are captured and accessible via `fatbaby_read_log("entity-graph")`
- No changes to behavior of the existing six processes
