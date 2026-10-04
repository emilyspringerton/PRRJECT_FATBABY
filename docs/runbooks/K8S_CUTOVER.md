# Runbook — FatBaby systemd → Kubernetes core pod (zero missed polling windows)

Companion to `docs/northstar/FATBABY_K8S_UDS_NORTHSTAR.md`. Founder bar (S498): "a cutover with ZERO
DOWNTIME ... i dont want to miss a single 15 second polling window." Nothing here has been run
against a real cluster yet (no credentials / no schedulable nodes from the authoring box); steps
marked **[gate]** must pass before the next one starts.

## 0. Preconditions (all currently open)

| Gate | Card | State |
|---|---|---|
| `gcloud auth login` done; cluster `prrject-fatbaby` schedules nodes (was 0 nodes for 32 h+) | K8S-FB-09 | blocked on a human |
| Billing budget alerts (25/50/90 %) exist | K8S-FB-09 | not done |
| Image built + pushed to `us-central1-docker.pkg.dev/project-d24a71e9-2daf-4b2d-917/emily/fatbaby:<tag>` (Alpine/musl link unproven until here) | K8S-FB-11 | blocked: no Docker here |
| `accuracy.ndjson` deduped (15 GB / 58.5 M lines, ~100× duplicates; PVC is 20 Gi) | K8S-FB-13 | open — see §2 |
| Secret `fatbaby-env` applied out of band (optional; none exist on the box today) | K8S-FB-07 | tooling done |

## Status 2026-10-04 (parallel run LIVE; DNS/Ingress untouched, box still running)

- Cluster `prrject-fatbaby` (Autopilot, us-central1) schedules nodes; Artifact Registry repo `emily` + Cloud Build enabled;
  `scripts/build-image.sh` builds `fatbaby:0.1.0` (musl link proven). Apple/budget alerts (25/50/90 %) are STILL not set.
- PVC `fatbaby-var` = 20Gi GCE persistent disk (`standard-rwo`), seeded from the box (~1.1 GB, deduped `accuracy.ndjson`
  16 GB -> 80 MB, sha256-verified). `fatbaby-core` pod 19/19 Running, 0 restarts after memory right-sizing.
- Found while seeding (real, pre-existing): `pr-reaction/events/2026-09-14.ndjson` and `prwatch-body/events/2026-09-14.ndjson`
  each hold ONE torn line (truncated record glued in front of a complete one). The box's `prwatch-body` and
  `pr-reaction-watcher` have been DEAD since ~2026-09-14 because of it. Repaired on the PVC copy only (box files untouched).
- GKE Autopilot gotchas fixed: per-container ephemeral-storage defaults to 1Gi (pod cap 10Gi) -> renderer now emits 256Mi;
  memory limits sized from idle box RSS OOM'd on real data (newssite ~0.7 GB, signalapi ~0.75 GB) -> raised.
- `cutover-verify.sh` shows 3 events of depth difference (5003 new vs 5006 old); box has produced no events since the
  weekend started, so this is index-depth drift, not missing data - re-run on a trading day before the DNS switch.
- NEXT: soak through Monday's trading day with hourly verify, budget alerts, then section 4 (Ingress + DNS).

## 1. Why the cutover is group-level, not per-process

Processors share one event store and talk over unix sockets inside one pod, so the pod is the unit:
you cannot run `processor` in k8s against `secwatch` on the box. The old and new stacks run **side by
side, each with its own store**, and converge by content, not by sequence number.

- **Producers** (`secwatch`, `prwatch`, `prwatch-body`, ...) poll external APIs. Running both is safe:
  every event has a stable ID and `LoadSeenIdentities` dedups against the store itself, so a gap between
  the snapshot and pod start is re-discovered on the first poll (EDGAR returns recent filings). The one
  exception is a feed with a short window (PR Newswire RSS): **seed the PVC from a snapshot taken minutes
  before the pod starts**, never hours.
- **Consumers** read their own store, so they see exactly what that store's producers wrote.

## 2. Seed the PVC

1. Dedupe first: `awk '!seen[$0]++' var/entity-graph/accuracy.ndjson > accuracy.dedup.ndjson` (streaming; needs
   only the unique set in RAM; a 15 GB read — run under `ionice -c3 nice -n19`). Verify unique-line count,
   then swap in at cutover time only (the live file is appended to by `entity-graph`). Fix the writer to
   dedupe before append (K8S-FB-13) or it regrows.
2. Copy everything else (≈ 1.2 GB: `secwatch`, `prwatch`, `prwatch-body`, indexes, `eps`, `guidance`, ...) to the PVC
   with a one-shot loader pod mounting `fatbaby-var`. Exclude `logs/` (557 MB, not state) and `emily-observations/`.
3. **[gate]** Sizes match: `du -sb` per directory, box vs PVC.

## 3. Parallel run

1. `parena-gitops` applies `EMILY/gitops/clusters/prrject-fatbaby/20-fatbaby-core.yaml` (commit = deploy).
   Do not change DNS; `fatbaby.io` still points at the box.
2. **[gate]** All 19 containers `Running`, none restarting: `kubectl -n emily get pod -l app=fatbaby-core`.
3. **[gate]** Sockets are live: signalapi/newssite logs show `also listening on unix:///run/fatbaby/...`.
4. Wait for one full poll of every producer (secwatch 5 m, prwatch 30 s, slowest watcher 6–24 h are
   not waited on — they are idempotent), then compare content:
   `scripts/cutover-verify.sh http://127.0.0.1:9091 http://127.0.0.1:19091` (second URL via
   `kubectl -n emily port-forward svc/fatbaby-core 19091:9091`). **[gate]** exit 0 (`MATCH`).
   Exit 1 = new side missing/extra data: do **not** proceed. Exit 2 = a side is unreachable.
5. Soak ≥ 1 full trading day with the verifier run hourly; every run must be `MATCH` (allowing the new side
   to lag by at most one poll interval — re-run before declaring a mismatch).

## 4. Traffic switch

1. Add the real hosts to the single edge Ingress (`gitops/specs/edge.ingress`, already lists `fatbaby.io`
   and `api.fatbaby.io`); wait for the managed LB to provision and the Ingress to show an address.
2. Lower the DNS TTL for both names to 60 s a day ahead. Point DNS at the Ingress IP. **[gate]** `curl
   https://fatbaby.io/` and `https://api.fatbaby.io/v1/health` return 200 from the pod (compare
   `uptime_seconds` to the pod's age).
3. Keep the systemd stack running (still producing, now unreachable) for ≥ 24 h. Rollback = point DNS back.

## 5. Decommission

Only after §4's 24 h: `systemctl --user stop` each `fatbaby-*` unit, one at a time, re-running the verifier
after each stop is unnecessary (they no longer serve traffic) but check `kubectl logs` for the pod's own
errors. Do **not** delete `var/` on the box until the first GCS backup of the PVC contents is verified.

## Not covered (named, tracked)

- `movers-watcher` / `bond-watcher` are systemd timers; a CronJob cannot reach the pod's sockets (K8S-FB-14).
- `broker`, `kgraph-server` stay on the box for now (path coupling to `EMILY/` and `gpt2-alpine-c`).
- Splitting the pod (Redis Streams, K8S-FB-10) — the pod is one restart unit and uses `strategy: Recreate`
  (RWO volume), so a deploy is a short full restart; producers heal the gap by identity dedup.
