#!/usr/bin/env bash
# Build every k8s-core-pod binary (cgo ON: eventstore/seqlock is a PARENA-compiled cgo mod) into $1.
# Used by docker/fatbaby.Dockerfile and, without Docker, as the local proof that the build stage
# works. Asserts each binary links only glibc (libc/libm/libpthread/ld-linux) so it runs on
# distroless/base, which ships nothing else.
set -euo pipefail
OUT="${1:?usage: build-static-bins.sh OUTDIR}"
cd "$(dirname "$0")/.."
BINS=(secwatch prwatch prwatch-body processor entity-graph signalapi newssite eps-processor eps-reconciler
      guidance-watcher pr-indexer pr-reaction-watcher form4-watcher dividend-watcher buyback-watcher
      nt-watcher schd13-watcher earnings-calendar market-data-watcher)
mkdir -p "$OUT"
for b in "${BINS[@]}"; do
  CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o "$OUT/$b" "./cmd/$b"
  extra="$(ldd "$OUT/$b" 2>/dev/null | awk '{print $1}' | grep -vE '^(linux-vdso|libc\.so|libm\.so|libpthread\.so|libdl\.so|librt\.so|/lib.*ld-linux)' || true)"
  if [ -n "$extra" ]; then echo "UNEXPECTED SHARED LIBS in $b: $extra" >&2; exit 1; fi
done
echo "built ${#BINS[@]} binaries in $OUT"
