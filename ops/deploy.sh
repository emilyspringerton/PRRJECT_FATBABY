#!/usr/bin/env bash
# FatBaby production deploy script.
# Run from repo root: bash ops/deploy.sh
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN_DIR="$REPO_ROOT/bin"

echo "==> Building binaries..."
cd "$REPO_ROOT"
go build -o "$BIN_DIR/newssite"   ./cmd/newssite
go build -o "$BIN_DIR/processor"  ./cmd/processor
go build -o "$BIN_DIR/secwatch"   ./cmd/secwatch
go build -o "$BIN_DIR/signalapi"  ./cmd/signalapi
go build -o "$BIN_DIR/projector"  ./cmd/projector
echo "    binaries written to $BIN_DIR/"

echo "==> Running tests..."
go test ./...

echo "==> Ensuring log and var dirs..."
mkdir -p "$REPO_ROOT/var/logs" "$REPO_ROOT/var/secwatch" \
         "$REPO_ROOT/var/commentary" "$REPO_ROOT/var/docindex" \
         "$REPO_ROOT/var/eps" "$REPO_ROOT/var/entity-graph"

echo "==> Reloading systemd units..."
sudo cp ops/systemd/*.service /etc/systemd/system/
sudo systemctl daemon-reload

echo "==> Restarting services..."
for svc in fatbaby-secwatch fatbaby-processor fatbaby-signalapi fatbaby-newssite; do
    sudo systemctl restart "$svc"
    sudo systemctl enable  "$svc"
    echo "    $svc restarted"
done

echo "==> Reloading nginx..."
sudo nginx -t && sudo systemctl reload nginx

echo "==> Deploy complete. Check status:"
echo "    sudo systemctl status 'fatbaby-*'"
echo "    tail -f $REPO_ROOT/var/logs/*.log"
