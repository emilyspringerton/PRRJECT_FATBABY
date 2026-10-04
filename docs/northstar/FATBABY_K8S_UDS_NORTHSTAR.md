# NORTHSTAR — FatBaby off this node: one core pod, unix sockets inside it, GitOps out

Founder real-time, 2026-10-04: "we need to get fatbaby off of this node into kubernetes we need to
use unix domain sockets for same node interprocess communication for the processors" → "use
whatever we were using for the GOLDEN DOCS service use the same patterns whenever possible
GITOPS etc." Extends `KUBERNETES_MIGRATION.md` (GKE Autopilot, durable-queue decision) and
`EMILY/docs/KUBERNETES_SERVICE_MIGRATION_NORTHSTAR.md` (single Ingress, ClusterIP-only, cost
rules). Cards: kanban `K8S-FB-00..12`.

## The one fact that shapes everything

A unix socket only connects processes that share a filesystem path. In Kubernetes that means
**containers in the same Pod** (shared `emptyDir`). Autopilot forbids `hostPath`, so "same node,
different pod" UDS is not available. Good news: it is also not needed, because FatBaby's
processes are today coupled through the shared `var/` event store, and a `ReadWriteOnce` PVC can
only be mounted by pods on one node anyway. So the honest topology is:

| Layer | Mechanism |
|---|---|
| Between processors in the **core pod** | unix sockets in `emptyDir` at `/run/fatbaby/` |
| Core pod ↔ **internet / other services** | TCP, `ClusterIP` Service, the **one** shared Ingress |
| Core pod ↔ **other pods** (future, after peel-off) | Redis Streams (S498) + IDUNA-JWT HTTP, not sockets |

Rule: **a listener is TCP only if something outside the pod must reach it** (newssite `:8082`
for the Ingress). Everything else listens on `unix:///run/fatbaby/<name>.sock`.

## Real couplings found (checked in code)

- `newssite → signalapi` reverse proxy (`internal/newssite/handler.go` hardcoded `127.0.0.1:9091`)
- `movers-watcher → newssite` `POST /api/commentary`
- `newssite → emily-agent` (asklily) and `emily-agent → newssite` (status tools)
- every processor ↔ every other via files in `var/<name>/` and 15–30 s `Tail` polling

## Plan

1. `internal/udsipc` — listen/dial/HTTP-over-UDS helpers (K8S-FB-01).
2. Move the HTTP couplings to `unix://` URLs; `-listen` flag accepts `unix:///path` (FB-02).
   Dual-mode: works on the systemd box today, so it ships and soaks before any cutover.
3. `unixgram` append-notify: writers wake tailers instantly, polling stays as the fallback (FB-03).
4. Pod simulator: run the pod's containers as isolated processes on a shared socket dir (FB-04).
5. Image + manifests follow the **golden-docs pattern** exactly (FB-05/06/07/12):
   - image: `Dockerfile.collections` shape — golang build stage, small nonroot runtime, one image,
     per-container `command` — but on **Alpine** (founder: "can we use alpine?"): a shell for
     `kubectl exec`/exec probes, same base family as gpt2-alpine-c. The cgo `seqlock` mod means musl
     with `CGO_ENABLED=1` (not static); needs `ca-certificates` + `tzdata` added explicitly.
   - manifests: rendered by `parena-k8s-render` (PARENA-first; extended for multi-container pods),
     committed to the manifests repo, applied by the pull-based `parena-gitops`. No cluster
     credentials in CI.
   - one Ingress via `ingress-yaml`; all Services ClusterIP; explicit resource requests.
6. Cutover runbook with parallel run and ID-dedup verification (FB-08).
7. Blocked, tracked in backlog: `gcloud auth` + a schedulable cluster (FB-09), Redis wiring to peel
   processes out of the shared PVC (FB-10), image push + staged cutover (FB-11).

## Honest limits

- No Docker, no musl toolchain, no `kubectl` credentials, no schedulable cluster from this box: the
  Alpine/musl link is unproven until the first real `docker build`; manifests and image
  are **unverified against a real cluster**; the pod simulator proves the socket wiring, not
  Kubernetes scheduling.
- A single core pod is a single restart unit and bills the sum of its containers. It is the
  smallest change that works with file-coupled processes; Redis Streams is how it gets split.
- UDS has no network-level auth; access control is socket file mode 0660 plus `SO_PEERCRED`
  checks on the sensitive ones, inside a pod whose containers already share a trust boundary.
