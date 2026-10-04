#!/usr/bin/env bash
# Build+push the fatbaby image with Cloud Build (no local Docker needed).
# usage: scripts/build-image.sh [tag]   (default tag 0.1.0)
# Assembles a context holding PRRJECT_FATBABY + the sibling modules go.mod `replace`s (SKULDMARK, NORN),
# tracked files only (git ls-files), so var/ (17 GB of live state) never uploads.
set -euo pipefail
TAG="${1:-0.1.0}"
PROJECT="${GCP_PROJECT:-project-d24a71e9-2daf-4b2d-917}"
IMAGE="us-central1-docker.pkg.dev/$PROJECT/emily/fatbaby:$TAG"
HERE="$(cd "$(dirname "$0")/.." && pwd)"; PARENT="$(dirname "$HERE")"
CTX="$(mktemp -d)"; trap 'rm -rf "$CTX"' EXIT
for r in PRRJECT_FATBABY SKULDMARK NORN; do
  mkdir -p "$CTX/$r"; (cd "$PARENT/$r" && git ls-files -z | xargs -0 -I{} cp --parents {} "$CTX/$r/")
done
cp "$HERE/docker/fatbaby.Dockerfile" "$CTX/Dockerfile"
gcloud builds submit "$CTX" --tag "$IMAGE" --project "$PROJECT"
