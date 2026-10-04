# fatbaby image — ONE image holding every pipeline binary; the k8s core pod picks one per container
# via `command:` (EMILY/gitops/specs/fatbaby-core.pod). Same shape as EMILY/emily-agent/
# Dockerfile.collections: golang build stage, distroless nonroot. One real difference: collections is
# pure Go (static), but eventstore/seqlock is a PARENA-compiled cgo mod (flock critical section), so
# these binaries link glibc — CGO_ENABLED=1 in the golang (Debian bookworm) stage, run on
# distroless *base* (has glibc 2.36, same as the build stage), not *static*.
# UNVERIFIED as an image: no Docker in the authoring sandbox. scripts/build-bins.sh proves the
# part that can break without Docker (every binary builds with cgo and links only libc).
#
# build (repo root as context):  docker build -f docker/fatbaby.Dockerfile -t <registry>/fatbaby:<tag> .
FROM golang:1.25-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN scripts/build-bins.sh /out/bin

FROM gcr.io/distroless/base-debian12:nonroot
WORKDIR /app
COPY --from=build /out/bin /app/bin
# Config is baked in (golden-docs pattern): a watchlist change is a new image tag rolled out by GitOps.
COPY config /app/config
# var/ is the shared RWO PVC mounted at /app/var; /run/fatbaby is the shared emptyDir (unix sockets).
ENV FATBABY_ROOT=/app
USER nonroot
