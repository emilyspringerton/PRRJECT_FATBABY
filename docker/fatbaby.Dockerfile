# fatbaby image — ONE image holding every pipeline binary; the k8s core pod picks one per container
# via `command:` (EMILY/gitops/specs/fatbaby-core.pod). Same shape as EMILY/emily-agent/
# Dockerfile.collections (golang build stage, small nonroot runtime) but on Alpine, per founder
# ("can we use alpine?"): a shell + busybox in the pod for `kubectl exec` debugging and exec probes,
# which distroless has none of, and the same base family as gpt2-alpine-c / the PARENA Alpine distro.
#
# musl, not glibc: eventstore/seqlock is a PARENA-compiled cgo mod (flock critical section), so the
# build uses CGO_ENABLED=1 with Alpine's gcc+musl and the binaries link libc.musl. seqlock_host.c is
# plain POSIX (fcntl/flock/unistd), nothing glibc-specific.
# UNVERIFIED as an image AND as a musl build: no Docker and no musl toolchain in the authoring
# sandbox. scripts/build-bins.sh proves the Go side (all 19 build, cgo, libc-only) on glibc; the musl
# link is only proven by the first real `docker build`.
#
# build (repo root as context):  docker build -f docker/fatbaby.Dockerfile -t <registry>/fatbaby:<tag> .
FROM golang:1.25-alpine AS build
RUN apk add --no-cache gcc musl-dev bash
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN scripts/build-bins.sh /out/bin

FROM alpine:3.22
# ca-certificates: every watcher polls HTTPS (SEC EDGAR, PR Newswire) — unlike distroless, Alpine
# ships none. tzdata: market-calendar code loads America/New_York.
RUN apk add --no-cache ca-certificates tzdata \
 && adduser -D -u 65532 -h /app nonroot
WORKDIR /app
COPY --from=build /out/bin /app/bin
# Config is baked in (golden-docs pattern): a watchlist change is a new image tag rolled out by GitOps.
COPY config /app/config
# signalapi opens ./migrations/mysql relative to WORKDIR for its SQLite read model (found by internal/podsim).
COPY migrations /app/migrations
# var/ is the shared RWO PVC mounted at /app/var; /run/fatbaby is the shared emptyDir (unix sockets).
# uid 65532 matches the pod's fsGroup (stdlib/k8s/pod.prn) so both volumes are writable.
ENV FATBABY_ROOT=/app
USER 65532
