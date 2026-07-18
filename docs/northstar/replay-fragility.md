# NORTHSTAR: Index Persistence — Ending the Full-History Replay on Every Restart

**Status:** Draft v0.1
**Date:** 2026-07-18
**Fits into:** `eventstore` (read path) → `signalapi` / `newssite` / `entity-graph` (the three
full-replay consumers) → systemd supervision (`ops/systemd/*.service`, the restart pattern this
must survive)
**Founder framing, verbatim:** "we need to fix the entity graph into memory thing, i dont know
what to do."

---

## 1. Premise

Three long-running processes — `cmd/signalapi`, `cmd/newssite`, and `cmd/entity-graph` — treat
the event store (`var/secwatch/events/*.ndjson`, currently 630MB / 116,431 records across 15
daily journal files) as their only source of truth and rebuild their entire in-memory working
state from it on every cold start. Nothing about the built state is ever persisted. On a 3.8GB
host with a 496MB swap partition, this failed live twice in one night:

- **2026-07-17 (evening):** a fresh `signalapi` rebuild started at ~2,000-2,500 records/sec,
  degraded to ~60-70/sec around record ~100,000, then apparently stalled at ~109,000-109,559 for
  minutes while RSS climbed 128MB → 628MB and swap went from ~57MB free to ~4MB free. Manually
  stopped and the unit disabled. It is still disabled as of this writing — signalapi is down
  pending this fix.
- **2026-07-18 ~05:27-05:38 UTC:** reboot-recovery `emily start --all` restarted it; identical
  stall at the same ~109,000 mark, RSS 540MB, swap 43% used, killed again. Fully reproducible on
  every cold start at the current store size.

The same night, `newssite` OOM-crash-looped every ~7 minutes (each restart = another full
replay), intermittently telling users "we don't cover AMZN" for tickers with real data — the
index simply hadn't finished rebuilding yet.

Under Principle 15 (`EMILY/docs/THE_EMILY_WAY.md`, "Operational Health Is Not Optional") and the
SECTION 152 posture, supervised restart is a *normal, frequent* event — crash, OOM-kill, deploy,
host reboot. A design where every restart costs a full-history replay turns the safety mechanism
(auto-restart) into the failure mode (restart storm). That is the actual bug. The fix is not
"restart less"; it is "make restart cheap."

**The decision this document makes:** persist each process's index as a SQLite checkpoint
(snapshot-plus-tail), and fix the event-store read path whose accidental O(n²) behavior is what
actually turned a 630MB scan into a swap death spiral. Mongo is explicitly evaluated and
rejected for this (§5). Bounded replay windows are rejected as the primary fix (§5). A shared
indexing layer is named as the correct long-term shape but not this build (§8).

## 2. What was actually found (read the code, not the vibes)

Three distinct pathologies, all real, all verified against the running store:

### 2a. Full-history replay from sequence 1 on every start

- `internal/signalindex/builder.go` `Build()` — scans from `fromSeq := uint64(1)` in pages of
  512 via `store.ReadFrom` until exhausted.
- `internal/newssite/docindex/docindex.go` `Build()` (line ~182) — identical pattern, identical
  page size. Holds 11,663 `DocSummary` records (one per `source_document_persisted` event),
  each with an ~800-rune `BodyPreview`, in two maps (`byTicker`, `byIdentity`).
- `cmd/signalapi/main.go` runs **both** of the above sequentially at startup (lines 49 and 56)
  — two full-store scans back to back before it will bind its port.
- `cmd/newssite/main.go` (lines 169-180) also runs both, concurrently with serving (it binds
  first — which is why its failure mode was "serving wrong answers" rather than "not up").

### 2b. The event-store read path is accidentally O(n²) — this is the stall

`eventstore/file_store.go` `ReadFrom()` answers every 512-record page request by calling
`readRecordsFromFile(p)` — which **reads and JSON-decodes the entire journal file into a
`[]Record` slice** — on every file that might contain the cursor. The `fileMaxSeq` cache
(line 35) only lets it skip *closed* files entirely *behind* the cursor. The file currently
being scanned is re-read and re-parsed in full for every page taken from it.

Ground this against the real store. Journal files are daily and wildly uneven:

| File | Records | Size | Starts at seq |
|---|---|---|---|
| 2026-05-30 … 2026-06-23 (12 files) | ~96,500 total | ~275MB | 1 |
| **2026-07-16.ndjson** | **19,933** | **354MB** | **95,456** |
| 2026-07-17 / 07-18 | 3 | ~1MB | 108,601 |

The 2026-07-16 file averages ~17.8KB/record (full cleaned source-document text). Scanning it in
512-record pages means **~39 complete read-and-parse passes over a 354MB file — ~13.8GB of
redundant JSON decoding — and each pass materializes the whole file as a `[]Record` (~400MB+ of
transient allocations) while holding the store mutex.** On a 3.8GB box already carrying
entity-graph's ~590MB, that is exactly the observed curve: fast through the small early files,
~60/sec once inside the big file, then GC/paging collapse. The observed "stall at ~109,000" is
seq ~13,500 *into* the July 16 file — mid-file, deep in the re-read regime. It was never a hang;
it was quadratic I/O meeting a full swap partition.

Worse, this cost is not startup-only: the `Tail()` pollers (30s interval, `ReadFrom(latest+1,
256)`) hit the same path — the *current day's* journal is deliberately never cached
(file_store.go line 145-150), so **every 30-second tail poll re-reads and re-parses the entire
current-day file**. On July 16 that meant re-parsing a growing 354MB file every 30 seconds, all
day, in every tailing process simultaneously. (Related already-landed fix: `b731804` stopped
`newssite` `serveDoc` from full-store-scanning per article view — same family of bug.)

### 2c. entity-graph: cursor exists, but per-batch full reloads anyway

`cmd/entity-graph/main.go` is *better* than the other two in one way — it persists a cursor
(`var/entity-graph/.cursor`, currently 108,602) and never replays the event store for its main
loop. But `runBatch()` (runs every 30s poll whenever ≥1 new record exists):

1. Calls `buildFilingIndexes()` (line 661) — a **full event-store scan from seq 1**, through the
   same O(n²) `ReadFrom` path, to rebuild the filing-date/form recovery maps. Every batch.
2. Reloads and dedupes **all of `var/entity-graph/accuracy.ndjson`** — append-only, currently
   **482,696 lines / 136MB** (up from ~345K at last count), growing without bound because each
   batch recomputes correlation records over the full deduped signal history and *re-appends
   them all* (`WriteAccuracyRecords`, main.go line 606). The file is duplicates of duplicates;
   `BuildAccuracyReports` dedupes on load, so the numbers stay right while the load cost and
   file size grow forever.
3. Reloads `nodes.ndjson` (after a `CompactNodes` rewrite pass), `signals.ndjson` (3.4MB, deduped
   on load), and `health_history.ndjson` — from scratch, every batch, into a fresh `Graph`.

Its ~590MB steady-state RSS is real accumulated working data plus the churn of rebuilding these
structures every 30 seconds. The fix for entity-graph is not "index less" — the accuracy history
is the RSI calibration input and the 11.9%-precision finding depends on the full resolved
record. The fix is: stop re-deriving identical state from ever-growing append-only files on
every batch.

## 3. The decision

**Primary direction: per-process SQLite checkpoint (snapshot-plus-tail), on top of a repaired,
streaming event-store read path.** Concretely:

1. **Phase 0 — fix `ReadFrom`'s quadratic re-read with a streaming scan API.** Mandatory
   regardless of any persistence choice: even a snapshot design must replay *some* tail, and
   today replaying even one day's tail through a 354MB journal is pathological. This alone turns
   the full cold rebuild from ~14GB of redundant parsing into a single ~630MB pass with bounded
   memory — likely tens of seconds, and it makes the 30-second tail polls near-free.
2. **Phase 1 — signalapi + newssite persist their indexes to a local SQLite file** with a
   `latest_seq` watermark; startup = load rows (subsecond at 11.6K docs + ~5K signals) + replay
   only events newer than the watermark. Restart cost becomes proportional to *downtime*, not to
   *history*.
3. **Phase 2 — entity-graph** gets the same checkpoint treatment for its three per-batch reload
   sins: the filing-date/form index becomes an incrementally-maintained table (no more per-batch
   full-store scans), accuracy records get a keyed table with upsert dedup (killing the 482K-row
   duplicate file), and the graph loads once per process lifetime instead of once per batch.

Why this is one decision and not three: all three processes share the same disease (derive
everything from append-only files, persist nothing derived) and the same house-pattern cure.
`modernc.org/sqlite` is already in `go.mod` (pure Go, no CGO, nothing new to install);
`internal/store/sqlite.go` already opens SQLite databases; `signalapi` already maintains
`var/signalapi.db` as its read-model fallback; `IDUNA/internal/blog` and
`IDUNA/internal/mailinglist` — built the same night as this incident — each use their own small
SQLite file rather than a new database dependency. Even the code agrees:
`internal/signalindex/index.go` line 15 has carried this TODO since it was written:

> `// TODO(scale): Keep this as the migration seam for a future SQLite-backed index when startup
> scans exceed acceptable latency.`

Startup scans have exceeded acceptable latency. This document is that TODO coming due.

## 4. Design

### 4a. Phase 0: streaming scan in `eventstore`

Add to `eventstore.EventStore` (implemented by `FileStore`):

```go
// Scan streams every record with Sequence >= fromSeq to fn, in order,
// reading each journal file exactly once, line by line, without ever
// materializing a whole file. fn returning an error stops the scan.
Scan(ctx context.Context, fromSeq uint64, fn func(Record) error) error
```

Implementation notes, all inside `file_store.go`:

- Line-streaming decode (`bufio.Scanner` with a large buffer — records reach ~1MB+ — or
  `bufio.Reader.ReadBytes`), decode one `Record`, invoke `fn`, discard. Peak memory = one
  record, not one file.
- Keep the `fileMaxSeq` skip cache; additionally, for the *current* journal, remember the byte
  offset reached at the end of each scan keyed by `(path, size)` so a tail poll only reads bytes
  appended since the last poll instead of re-parsing the whole current-day file every 30s.
- Do **not** hold `s.mu` across the whole scan (today's `ReadFrom` blocks `Append` for the full
  page read). Take the mutex only to snapshot the journal path list and current-file identity;
  files are append-only, so concurrent reads of closed bytes are safe, and the truncated-final-
  line tolerance already in `readRecordsFromFile` carries over for the racing tail case.
- Migrate `signalindex.Build`, `docindex.Build`, `entity-graph`'s `buildFilingIndexes`, and both
  `Tail` pollers to `Scan`. `ReadFrom` stays for targeted small fetches (e.g. newssite's
  detail-page `ReadFrom(seq, 1)`), but nothing iterates history through it anymore.
- **Acceptance:** cold `signalapi` full rebuild (checkpoint deleted) completes in under 60s with
  RSS under 300MB, on this box, at current store size. A tail poll with no new events does no
  full-file read (verify by timing/strace, not by faith).

### 4b. Phase 1: checkpoint store for signalapi + newssite

New package `internal/indexcheckpoint` (name TBD), owning one SQLite file per process —
`var/signalapi-index.db`, `var/newssite-index.db`. Deliberately **per-process files, not one
shared file**: SQLite's single-writer model and these processes' independent lifecycles make
sharing a file a new coupling for zero benefit. Schema:

```sql
CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT);
-- keys: schema_version, latest_seq, snapshot_at

CREATE TABLE docs (            -- mirrors docindex.DocSummary
  identity TEXT PRIMARY KEY, ticker TEXT, source_type TEXT, form TEXT,
  document_url TEXT, body_preview TEXT, char_count INTEGER,
  filing_date TEXT, persisted_at TEXT, seq INTEGER
);
CREATE INDEX docs_ticker ON docs(ticker);

CREATE TABLE signals (         -- mirrors signalindex.SignalEntry
  ticker TEXT, accession TEXT, seq INTEGER, form TEXT, filing_date TEXT,
  signal_type TEXT, importance INTEGER, sentiment REAL, summary TEXT,
  impact_analysis TEXT, ts TEXT, appended_at TEXT, raw_metadata TEXT,
  PRIMARY KEY (ticker, accession)  -- v2-replaces-stub semantics for free
);
```

Lifecycle:

- **Startup:** open DB → check `schema_version` → `SELECT` all rows into the existing in-memory
  `Index` structs (the serving structures and every handler stay exactly as they are — this is a
  bootstrap change, not a query-path change) → `Scan(latest_seq+1, ingest)` to catch up → serve.
- **Steady state:** the existing `Tail` goroutine, after ingesting a poll's records into memory,
  writes the same records to SQLite in one transaction and updates `meta.latest_seq`. Volume is
  a handful of rows per 30s — negligible. Crash between event-store append and checkpoint write
  is safe by construction: the watermark only advances with the transaction, and replay re-derives
  anything newer (`Ingest` is already idempotent by identity/accession).
- **Self-healing:** open failure, integrity failure, or `schema_version` mismatch → log loudly,
  delete the file, full rebuild via `Scan` (tolerable post-Phase 0), write a fresh checkpoint.
  The checkpoint is a **disposable cache of the event store, never a source of truth** — the
  event store remains the only authority. Deleting `var/*-index.db` must always be a safe
  operator move, and gets documented as such in the ops runbook.
- Index struct changes just bump `schema_version` — the cost of a version bump is one full
  rebuild on next start, which Phase 0 made acceptable. No migration framework for a cache.
- **Acceptance:** restart `signalapi` with a warm checkpoint → serving in under 5 seconds with
  correct `Depth()`/`latest_seq`; `systemctl --user enable --now fatbaby-signalapi.service`
  (currently disabled pending exactly this) survives three consecutive rapid restarts without
  RSS exceeding its `MemoryMax=900M` or swap moving. Same for `newssite`: no "we don't cover
  AMZN" window on restart beyond the seconds of tail replay.

### 4c. Phase 2: entity-graph

Same checkpoint file pattern (`var/entity-graph/index.db`), attacking its three per-batch
reloads in order of pain:

1. **Filing index:** `filings(identity PK, filing_date, form)` maintained incrementally from
   `filing_discovered` events as they pass the *existing* cursor. `buildFilingIndexes`'s
   full-store scan runs exactly once (backfill migration into the table), then never again.
   This deletes a per-batch full read of a 630MB store — the single largest recurring waste on
   the box.
2. **Accuracy records:** `accuracy(record_key TEXT PRIMARY KEY, ...)` where `record_key` is the
   natural identity `BuildAccuracyReports` already dedupes on (signal_id + correlation type +
   outcome — confirm exact key from `accuracy.go` §dedup before building). Batches upsert
   instead of blind-appending; loading becomes a keyed table read of ~unique records (tens of
   thousands) instead of parsing 482K duplicate NDJSON lines. One-time migration imports the
   deduped legacy file. **Verify before/after `BuildAccuracyReports` output is byte-identical
   (same precision numbers, same 11.9% finding) — calibration must not shift as a side effect
   of storage.** Keep the legacy `.ndjson` untouched as the archival record until the founder
   retires it; verification against it stays possible.
3. **Graph lifetime:** hoist `NewGraph` + `LoadNodes`/`LoadAuditors`/`LoadSignals`/
   `LoadHealthHistory` out of `runBatch` to process start; batches mutate the long-lived graph
   and flush incrementally, as `FlushNodes`/`FlushEdges` already do. `CompactNodes` moves from
   every-batch to startup-only. This is a behavior-preserving hoist, but it touches the
   process's core loop — it lands last, alone, with the accuracy-report equivalence check as
   its regression gate.

Expected effect: entity-graph's per-batch cost drops from "reload the world" to O(new events),
its steady-state RSS drops (much of the current ~590MB is churn from rebuilding these
structures every 30s, held between GC returns), and its unit file
(`ops/systemd/fatbaby-entity-graph.service`, written 2026-07-18, never enabled) can finally be
enabled — a restart stops being something this process needs to fear.

## 5. Alternatives evaluated (and why not)

**Mongo as the index cache** (the founder's instinct: "cache into mongo or something") —
rejected for this problem on this box. `mongod`'s WiredTiger cache wants 256MB+ resident on a
host that spent this week at 280MB free with a full swap partition; it is a new always-on
process to supervise, exactly what SECTION 152 spent a night teaching us not to add casually;
and everything Mongo would provide here (a keyed, incrementally-updated, restart-surviving
materialization) SQLite provides in-process with a dependency already in `go.mod` and a pattern
already proven in three sibling services. Mongo remains what it already is in this codebase — an
*optional* mirror (`mongowriter`, `MONGODB_URL` unset = graceful no-op) — not load-bearing
startup infrastructure. See §7 for where this changes.

**Gob-encoded snapshot file** — seriously considered; simpler than SQLite on first contact.
Rejected because it rewrites the entire snapshot on every save (fine at 40MB, hostile at
entity-graph scale and to this host's I/O), is opaque to inspection (`sqlite3 var/signalapi-
index.db 'select count(*) from docs'` is an ops affordance, a gob blob is not), and couples the
snapshot's validity to Go struct encoding details. Incremental transactional upsert is the
actual shape of the problem; gob makes that shape awkward, SQLite makes it native.

**Bounded replay window as the primary fix** — rejected. It is a data-loss policy wearing a
performance-fix costume: `newssite`'s ticker/archive pages are explicitly full-history surfaces
(the incident's user-visible symptom was *already* "real data missing"); entity-graph's
precision calibration is defined over the full resolved history; and the win decays as the
window fills. Kept only as an *emergency lever*: Phase 0 adds a `-replay-from-seq` flag to
signalapi/newssite, default 1 (full), so a 3am incident has a documented degraded mode that
gets a service up with recent data first. A flag is a choice; a hardcoded window is a policy.

**"Just enable the systemd units and let MemoryMax handle it"** — already disproven live. With
a 900M cap and a rebuild that transiently allocates the whole 354MB journal per page,
supervision converts the memory bug into a restart storm. Supervision is necessary (and stays);
it is not the fix.

## 6. Quality metrics

- **Cold start (empty checkpoint), current store:** signalapi serving < 60s, RSS < 300MB.
  Today: did not complete; killed at 628MB+/540MB twice.
- **Warm start (checkpoint present):** signalapi and newssite serving < 5s. entity-graph first
  batch < 30s. Restart cost scales with downtime, not history.
- **Steady state:** tail poll with no new events performs no full-file read; entity-graph batch
  cost O(new events); `accuracy.ndjson` growth curve goes flat (table row count ≈ unique
  records, not 482K and climbing).
- **Supervision closes:** `fatbaby-signalapi.service` re-enabled, `fatbaby-entity-graph.service`
  enabled for the first time, both surviving kill -9 + auto-restart within their `MemoryMax`,
  live-verified the way S152-03 verified prwatch.
- **The number that matters to Principle 15:** zero minutes of "service down pending a replay it
  can't afford" — signalapi has been in exactly that state since 2026-07-17.

## 7. Forward-compat: the dedicated-DB-node question

The founder is separately weighing a dedicated DB node (staying otherwise monolithic) and a
longer-term multi-host "Emily cluster." Out of scope here, but this design is deliberately
shaped for that world rather than against it:

- The checkpoint schema in §4b *is* a read-model schema. With a DB node available, the same
  tables move into MySQL (the `migrations/mysql/` + `mysqlToSQLite` translator pipeline already
  exists for exactly this dual-target pattern) or Mongo (where `docs/mongo-entity-schema.md`
  already sketches the entity side), and `meta.latest_seq` remains the watermark. The
  bootstrap-from-checkpoint + tail-from-watermark discipline transfers unchanged; only the
  driver behind the checkpoint package swaps.
- What a DB node changes is §8's answer, not this one: with real shared storage, maintaining
  *one* set of read models for all consumers stops being a coupling risk and becomes the
  obvious topology. Build the seam now (checkpoint package with a narrow interface), swap the
  backend then.
- What it does **not** change: Phase 0. A streaming, non-quadratic event-store read path is
  correct on every future topology, and the event store itself stays file-backed and
  git-adjacent per the existing architecture — this document does not propose moving the store.

## 8. The shared-indexing-layer question (asked directly)

Should these be three independent replays at all? No — in the limit, this codebase already
knows the answer: `cmd/projector` + the MySQL/SQLite read-model schema is a nascent "one
process maintains materialized views, everyone else queries them" layer, and signalapi already
partially consumes it. One maintained read model serving newssite, signalapi, and entity-graph
is the correct end-state and would make §4's per-process checkpoints an implementation detail
of a single component.

It is not the immediate fix, on purpose: it changes three processes' query paths at once, adds
a new single point of failure that all three then wait on (a worse blast radius than today's
independent fragility, unless done carefully), and the founder asked for the memory/restart
problem fixed, not the topology redesigned. The phased plan converges on it rather than
blocking on it: after Phases 0-2, each process has an explicit, versioned, disposable read
model with a watermark — consolidating those into one maintained layer (or onto the DB node,
§7) becomes a migration of well-defined pieces instead of an excavation of implicit in-memory
state. Write that northstar when the founder picks the DB-node direction; do not build it now.

## 9. Build order

- **Phase 0** — `eventstore.Scan` streaming API; migrate both `Build`s, both `Tail`s, and
  `buildFilingIndexes` onto it; mutex scope fix; `-replay-from-seq` emergency flag; acceptance
  timing run on this box. *Smallest change, deletes the O(n²), unblocks everything else. Land
  alone.*
- **Phase 1a** — checkpoint package + signalapi (the currently-down service; highest urgency).
  Re-enable `fatbaby-signalapi.service`; kill-test under supervision.
- **Phase 1b** — newssite on the same package. Kill-test; confirm no missing-ticker window.
- **Phase 2** — entity-graph: filings table, accuracy upsert table (+ one-time dedup migration
  + calibration-equivalence check), graph-lifetime hoist. Enable its unit for the first time.
- **Phase 3** — ops-runbook documentation of the "checkpoints are disposable" invariant, plus a
  `CheckPollerHealth`-style freshness check on `meta.snapshot_at` so a checkpoint that stops
  advancing gets detected in minutes, per Principle 15, not discovered during the next incident.

Each phase is independently shippable and independently revertible; every phase ends with the
existing suite green (`go test ./...`) plus the live kill-test on this box, because the entire
point of this work is behavior under restart, and that is only verifiable by restarting.

## 10. Open questions

- **Checkpoint write cadence for entity-graph's graph state:** per-batch flush of dirty nodes
  (current `FlushNodes` behavior, kept) vs. periodic full snapshot. Leaning per-batch dirty
  writes; needs a look at `Graph` mutation granularity during Phase 2 design.
- **The exact accuracy-record natural key** — must be read out of `BuildAccuracyReports`'s
  dedup logic, not guessed; the migration's equivalence check is the enforcement.
- **Journal file rotation policy:** the July 16 file is 354MB because rotation is daily, not
  size-based, and backfill days are enormous. Size-capped rotation (e.g. 64MB segments) would
  bound worst-case single-file cost and improve `fileMaxSeq` skip granularity. Cheap, but it
  touches `Append`/recovery paths — separate small change, not bundled into Phase 0.
- **Read-only store opens:** `NewFileStore` opens today's journal for append and rewrites the
  latest-sequence state file even in pure consumers (signalapi/newssite/entity-graph never
  `Append`). Harmless so far; an explicit read-only open mode would remove a standing
  multi-writer wart. Note it, don't block on it.
- **Body-preview weight:** 11,663 × ~800 runes lives in newssite/signalapi memory for list
  pages. Post-Phase-1 the checkpoint could serve previews from disk and shrink the in-memory
  struct to metadata only. Only worth doing if post-fix RSS numbers say so — measure first.

## 11. What this deliberately does NOT do

- No new database *server*, no new always-on process, no new failure mode to monitor — the
  entire mechanism lives inside the three processes that already exist, per the same lesson
  the live-feed northstar (§4, §9) drew from this same week.
- No change to the event store's authority: NDJSON journals remain the single source of truth;
  every checkpoint is rebuildable from them by deletion.
- No change to any HTTP handler, query semantics, or serving data structure — bootstrap and
  maintenance change; the indexes the handlers see do not.
- No bounded-history policy: full history stays available on every surface that has it today.
- No shared read-model service and no store migration — named (§7, §8) as the likely next
  chapter so the seam is cut in the right place, and explicitly not built here.
