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

# Found live (2026-10-05, SHANKPIT): the CI SA (github-ci, see EMILY/gitops/CI_SETUP.md) is
# deliberately scoped to cloudbuild.builds.editor/artifactregistry.writer, not a project
# Viewer/Owner. Two consequences: (1) unpinned staging dir triggers a project-scoped
# storage.buckets.list call the CI SA's bucket-scoped bindings don't satisfy (403, misleadingly
# reported as a serviceusage error) -- --gcs-source-staging-dir skips that list; (2) `builds
# submit` can't stream the default (outside-the-project) logs bucket -- --async skips the wait,
# we poll `builds describe` (status only, never logs) ourselves.
BUILD_ID=$(gcloud builds submit "$CTX" --tag "$IMAGE" --project "$PROJECT" \
  --gcs-source-staging-dir="gs://${PROJECT}_cloudbuild/source" \
  --async --format="value(id)")

echo "submitted build $BUILD_ID, polling for completion..."
while true; do
  STATUS=$(gcloud builds describe "$BUILD_ID" --project "$PROJECT" --format="value(status)")
  case "$STATUS" in
    SUCCESS) echo "fatbaby:$TAG"; exit 0 ;;
    FAILURE|INTERNAL_ERROR|TIMEOUT|CANCELLED|EXPIRED) echo "build $BUILD_ID: $STATUS" >&2; exit 1 ;;
    *) sleep 5 ;;
  esac
done
