# NORTHSTAR — Kubernetes Migration (Phase 5 of the Distributed Event Intelligence plan)

Founder real-time, 2026-08-30: "need to start the kubernetes migration im not even kidding look
up the plan in fatbaby all our logstreaming data is going to get projected into google cloud
storage" → "the data is already there."

**The plan already existed** — `docs/architecture-distributed-event-intelligence.md`'s own
Phase 5 ("Kubernetes transition") names Kubernetes as the control plane once workload fanout
includes multiple autonomous services, lists the real service inventory this pipeline already
has, and draws the stateless/stateful split every real k8s migration needs. This doc is that
Phase 5, made concrete for this session's own real target: **GKE**, not generic Kubernetes —
this org is GCP-based (the existing backup bucket, Vertex AI project), and the founder's own "the
data is already there" is real, not aspirational: `emily backup run --target fatbaby` already
uploads curated `PRRJECT_FATBABY/var/` state to `gs://project-d24a71e9-2daf-4b2d-917-backups`
(`emily.cli/cmd/backup.go`) — the durable-archive prerequisite Phase 3 of the parent doc calls
for is real, live, today, not blocked on this migration.

## Real, honest finding, checked before writing a plan around it

The parent doc's own recommended order is Phase 2 (dual-sink fanout) → Phase 3 (S3/GCS object
layout) → Phase 4 (canonical event envelope) → Phase 5 (Kubernetes). Checked directly, not
assumed: **Phase 2's own scaffolding exists in code** (`internal/eventsink/`: `sink.go`,
`fanout.go`, `file_sink.go`, `s3_sink.go` — a generic `S3Uploader` interface + a
partition-aware `BuildS3ObjectKey`, real tests) **but is not wired into any running `cmd/`
process** — no binary constructs an `S3Sink`, no env var names a live bucket for it. Every real
pipeline process today (`secwatch`, `processor`, `dashboard`, `feedserver`, etc.) still reads and
writes `./var/<process>/` directly, per the parent doc's own real "Phase 1 (current): single
node + local NDJSON" description — nothing has moved past Phase 1 for the pipeline's own live
event flow, independent of the separate, already-real `emily backup run` archival snapshot.

**Why this matters for Phase 5 specifically**: a k8s Pod's local filesystem is ephemeral by
default. A process that currently treats `./var/<name>/*.ndjson` as its own durability layer
loses that state on every pod restart/reschedule unless one of these is real before cutover:

1. A real, mounted **persistent volume** (GCE PD via a `PersistentVolumeClaim`) per stateful
   service — the honest, minimum-change path, real infra work but no application code change.
2. Or finishing the parent doc's own Phase 2 for real (wiring `FanoutSink` into each process,
   pointed at a real GCS bucket via a real `GCSSink` — `internal/eventsink/s3_sink.go`'s own
   `S3Uploader` interface is already GCS-XML-API-compatible per its own header comment, so this
   is a real adapter to write, not a rewrite) — the architecturally cleaner path the parent doc
   actually recommends, more real work before cutover.

**This doc does not resolve that choice unilaterally** — it's a real, load-bearing decision the
founder should make explicitly (Principle 1/"ask before adding material" territory): PVCs first
gets pods running on GKE sooner with less new Go code; finishing Phase 2 first gets the
architecturally-recommended shape but delays the actual migration. Real, phased plan below stays
useful either way — it's PVC-first by default (matching "start the migration" over "finish the
event bus first"), with the Phase 2 alternative named at the point it actually branches.

## Real, phased plan

**Phase 5.0 — containerize, prove one service.** Real "smallest real proof point" discipline this
monorepo already applies everywhere else (BURROW Phase 1, DUNG Phase 1): pick ONE representative
stateless-shaped service, write a real multi-stage Dockerfile, build it, run it locally against a
mounted `./var/` directory, confirm identical behavior to `go run`. `dashboard` is the real
candidate here — no external API keys required to start (`processor`/`secwatch` need
`config/watchlist.json` + live network access to actually do anything, `dashboard` just needs to
serve whatever's already in `var/`), and its own SSE behavior is easy to verify with a plain
`curl`. See `docker/dashboard.Dockerfile` (this same commit) — **real, honest, unverified in this
session**: no `docker` binary was available in the sandbox this was authored in, so this
Dockerfile has not actually been built or run yet. Real next step: build and run it for real on a
box that has Docker, before trusting it further.

**Phase 5.1 — real GKE cluster.** A real GKE Autopilot cluster (least ops overhead — Autopilot
manages node provisioning, matching this team's own small-team-real-leverage value from the main
`CLAUDE.md`) in the same GCP project the backup bucket already lives in. Needs real `gcloud`
authentication this session doesn't have (`gcloud auth list` returned "No credentialed accounts")
— real, honest blocker, not glossed over: either the founder runs `gcloud auth login` themselves
(interactive OAuth, can't be done non-interactively from here) or this step runs on a box that
already has a real service-account credential provisioned. Not attempted here.

**Phase 5.2 — persistent volumes for today's real stateful shape.** One `PersistentVolumeClaim`
per process that still reads/writes local `./var/<name>/`, sized generously (event logs grow),
`ReadWriteOnce` (single-writer, matching each process's own current single-instance model — no
process here is horizontally scaled today). Real, deliberate choice per the finding above:
un-blocks Phase 5 immediately without first finishing Phase 2's own GCS fanout wiring.

**Phase 5.3 — real Deployment + Service manifests, one process at a time.** `k8s/dashboard.yaml`
was the real, first, representative raw manifest — a `Deployment` (1 replica, matching today's
real single-instance-per-process model, no k8s-native horizontal scaling claimed) + the PVC from
5.2 + a `Service` exposing the SSE port internally. **Superseded, same day, by a real Helm
chart**: `charts/dashboard/` (`Chart.yaml`/`values.yaml`/`templates/*.yaml`) — generated from
PARENA's own new `stdlib/k8s`/`stdlib/helm` primitives (founder real-time: "write kubernetes
primitives into the stdlib and write helm support into parena"), real, YAML-syntax-validated, the
same real resource values as the raw manifest with image/replica count now real Helm-templated
`{{ .Values.X }}` fields instead of hardcoded. `k8s/dashboard.yaml` itself is kept, not deleted —
real, honest documentation of the pre-Helm shape, same "don't discard, supersede" discipline this
doc's own parent doc already applies to the local-NDJSON event log. Real, honest, unverified
either way: no live cluster to apply this against yet (blocked on 5.1), and no `helm` CLI in the
authoring session to run `helm lint`/`helm template` against it. The remaining ~15 processes in
the main `CLAUDE.md`'s own process table follow the same real chart pattern once this one is
proven — not templated out in bulk here, matching this repo's own "prove one, then repeat"
discipline rather than generating 15 unverified manifests at once.

**Phase 5.4 — cutover, one process at a time, old and new running in parallel first.** Real,
standard migration discipline: a process's new GKE deployment runs alongside its existing
systemd-managed instance, verified for real (matching output/behavior) before the systemd unit is
stopped — never a hard cutover. Order matters: stateless-shaped, lower-stakes processes first
(likely `dashboard`, `newssite`), the real data-critical ones last (`secwatch`, `eps-reconciler`
— Operational Health Is Not Optional, `THE_EMILY_WAY.md` Principle 15, applies directly here: an
undetected gap in SEC/PR ingestion during a botched cutover is exactly the class of incident that
principle exists to prevent).

## Real risks and open questions, named honestly

- **The PVC-vs-Phase-2 branch above is a real, undecided architectural choice**, not resolved by
  this doc — flagged for the founder, not guessed at.
- **Phase 5.0's Dockerfile is unverified** — no Docker available in the authoring session; real
  next step is building and running it for real before Phase 5.1 depends on it working.
- **No GKE cluster exists yet** — Phase 5.1 needs real `gcloud` authentication this session
  doesn't have.
- **Every process beyond `dashboard` still needs its own real Dockerfile + manifest** — this doc
  proves the pattern on one, it doesn't claim the other ~15 are done or even started.
- **Secrets** (`ANTHROPIC_API_KEY`, IDUNA agent secrets, etc.) currently live in plain env files
  (`EMILY/var/emily-secrets.env`, `IDUNA/var/agent-secrets.env`) — real, separate work needed
  before cutover: Kubernetes `Secret` objects (or GCP Secret Manager) replace that, not attempted
  here.

## Decision: GKE Autopilot over Standard (2026-09-03, kanban priority-queue card 432432423)

Priority-queue card 432432423 asked to decide Autopilot vs. Standard explicitly. Phase 5.1 above
already leaned Autopilot ("least ops overhead") when this doc was first written; this section
checks that lean against the real, current process inventory rather than leaving it as an
unexamined default, and makes the decision final.

**Real inventory checked**: `PRRJECT_FATBABY/CLAUDE.md`'s own process table lists 17 real
processes today (`secwatch`, `prwatch`, `prwatch-body`, `processor`, `dashboard`, `newssite`,
`feedserver`, `broker`, `signalapi`, `observation-watcher`, `eps-processor`, `eps-reconciler`,
`guidance-watcher`, `jon-agent`, `form4-watcher`, `dividend-watcher`, `buyback-watcher`,
`nt-watcher`) — every one a plain Go binary doing HTTP serving, TCP framing, or outbound polling
against local `./var/<name>/` disk state (Phase 5.2's PVC-per-process plan). **None of the 17
need anything Autopilot restricts**: no privileged containers, no `hostNetwork`/`hostPath`, no
DaemonSets, no custom node-level sysctls or kernel modules, no GPU/exotic hardware. The parent
architecture doc's own aspirational Phase 5 inventory (Kafka/Redpanda/NATS, Postgres, Redis,
vector DB) is real but **not yet deployed** — those are Phase 2-4 scaffolding-only items (see
"Real, honest finding" above), not part of today's actual cutover surface, so they don't weigh
against Autopilot for this decision; if/when a stateful bus is actually stood up, GKE Autopilot
supports StatefulSets + `Persistent Disk CSI` PVCs today (this isn't a "wait for Standard" gap
either — only genuinely host-level/privileged workloads are).

**One real, concrete future-facing flag, named honestly rather than silently missed**: this same
session's own `PARENA/docs/NATIVE_PCAP_NORTHSTAR.md` (native packet capture) needs a raw
`AF_PACKET`/`SOCK_RAW` socket, which needs `CAP_NET_RAW` — Autopilot does not grant elevated
Linux capabilities or `hostNetwork` to pods. **If** a future PRRJECT_FATBABY process ever needs
in-cluster raw packet capture (no such process exists or is planned today — this is a real,
checked non-issue for the current decision, not a hedge), that specific workload would need
either a Standard cluster/node pool or to run outside the cluster entirely. Not a reason to pick
Standard now — a reason to re-open this decision later if that specific need ever materializes.

**Cost model checked**: Autopilot bills per-pod requested vCPU/memory (with a per-pod minimum),
Standard bills per-node regardless of bin-packing efficiency. With 17 mostly-small, mostly
low-traffic processes, small deliberate pod resource requests keep Autopilot's per-pod billing
competitive with a Standard node pool that would otherwise sit under-utilized outside SEC/PR
filing bursts — matching this team's own "small-team-real-leverage" value already named in the
root `CLAUDE.md` and quoted in Phase 5.1 above.

**Decision, final**: **GKE Autopilot.** Confirms Phase 5.1's original lean rather than reversing
it — the real inventory check above found no disqualifying workload, and Autopilot's own
zero-node-management overhead is a genuine, real fit for this team's size. Phase 5.1 itself
(`S207-02` in `EMILY/BACKLOG.md`) stays open — this decision unblocks *what kind* of cluster to
provision, not the still-real, still-unresolved need for interactive `gcloud auth login` before
provisioning can happen at all.

## Related

- `docs/architecture-distributed-event-intelligence.md` — the parent plan this doc's own Phase 5
  makes concrete; Phases 2-4 (event bus, GCS object layout, canonical envelope) are real,
  separate, still-scaffolding-only work this doc found, not resolved.
- `PRRJECT_FATBABY/CLAUDE.md`'s own process table — the real, current 17-process inventory the
  Autopilot-vs-Standard decision above was checked against.
- `PARENA/docs/NATIVE_PCAP_NORTHSTAR.md` — the one real, named, currently-hypothetical workload
  shape (`CAP_NET_RAW`) that would force revisiting the Autopilot decision if it ever becomes real.
- `emily.cli/cmd/backup.go` — the real, already-live GCS backup mechanism ("the data is already
  there") this doc's own opening section cites directly.
- `EMILY/docs/THE_EMILY_WAY.md` Principle 15 (Operational Health Is Not Optional) — governs the
  real cutover order in Phase 5.4.
