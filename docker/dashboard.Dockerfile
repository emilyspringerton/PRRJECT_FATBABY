# docker/dashboard.Dockerfile — Phase 5.0's own real, first proof point (see
# docs/northstar/KUBERNETES_MIGRATION.md). dashboard picked as the representative service: no
# external API keys needed to start (unlike processor/secwatch, which need a live network + a
# real watchlist to do anything), and its own SSE behavior is trivially checkable with `curl`.
#
# Real, honest, unverified: authored in a session with no `docker` binary available -- build and
# run this for real before Phase 5.1 (a real GKE cluster) depends on it working.
#
# Build:  docker build -f docker/dashboard.Dockerfile -t fatbaby-dashboard .
# Run:    docker run -p 8080:8080 -v $(pwd)/var:/app/var fatbaby-dashboard
# Verify: curl -N http://localhost:8080/  (matching the same real SSE endpoint `go run
#         ./cmd/dashboard` already serves -- confirm identical behavior, not just "it starts")

FROM golang:1.25-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# CGO_ENABLED=0: a real, static binary -- no libc dependency in the final stage, matching every
# other Go binary this monorepo already ships as a plain, standalone executable (emily.cli,
# burrow, etc.), not something this Dockerfile is inventing.
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/dashboard ./cmd/dashboard

FROM gcr.io/distroless/static-debian12:nonroot AS run
WORKDIR /app
COPY --from=build /out/dashboard /app/dashboard
# var/secwatch is where cmd/dashboard's own real --data-dir flag defaults to (unmodified here --
# WORKDIR /app makes that resolve to /app/var/secwatch); Phase 5.2's own PersistentVolumeClaim
# mounts at /app/var in k8s/dashboard.yaml (this same commit) so that default keeps working
# unchanged, no new flag/env var invented for this container specifically.
EXPOSE 8080
ENTRYPOINT ["/app/dashboard"]
