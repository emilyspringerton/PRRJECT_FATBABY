#!/usr/bin/env bash
# Parallel-run verifier for the FatBaby systemd -> k8s cutover (docs/northstar/FATBABY_K8S_UDS_NORTHSTAR.md,
# docs/runbooks/K8S_CUTOVER.md). Compares the OLD signalapi (box) and the NEW one (pod) while both run:
#   - per-ticker signal counts must be equal (nothing missing on the new side, nothing extra),
#   - the new side's latest_seq may lag the old only because event sequences are per-store — so we
#     compare CONTENT (counts per ticker), never raw sequence numbers.
# usage: cutover-verify.sh OLD_BASE_URL NEW_BASE_URL [API_KEY]
#   e.g. cutover-verify.sh http://127.0.0.1:9091 https://api.fatbaby.io  (or a kubectl port-forward URL)
# exit 0 = identical content, 1 = mismatch (details on stdout), 2 = a side is unreachable.
set -euo pipefail
OLD="${1:?old url}"; NEW="${2:?new url}"; KEY="${3:-}"
exec python3 - "$OLD" "$NEW" "$KEY" <<'PY'
import json, sys, urllib.request
old, new, key = sys.argv[1:4]
def get(base, path):
    req = urllib.request.Request(base.rstrip('/') + path)
    if key: req.add_header('Authorization', 'Bearer ' + key)
    try:
        with urllib.request.urlopen(req, timeout=15) as r: return json.load(r)
    except Exception as e:
        print(f"UNREACHABLE {base}{path}: {e}"); sys.exit(2)
def counts(base):
    h = get(base, '/v1/health')
    s = get(base, '/v1/signals')
    out = {r['ticker']: r['signal_count'] for r in s.get('summary', [])}
    return h, out
(ho, co), (hn, cn) = counts(old), counts(new)
print(f"old: depth={ho.get('index_depth')} tickers={ho.get('tickers')}   new: depth={hn.get('index_depth')} tickers={hn.get('tickers')}")
bad = []
for t in sorted(set(co) | set(cn)):
    if co.get(t) != cn.get(t): bad.append((t, co.get(t), cn.get(t)))
if ho.get('index_depth') != hn.get('index_depth'): bad.append(('<index_depth>', ho.get('index_depth'), hn.get('index_depth')))
if bad:
    print("MISMATCH (ticker, old, new):")
    for b in bad[:50]: print("  ", b)
    print(f"{len(bad)} difference(s) — new side is missing/extra data; do NOT stop the systemd units")
    sys.exit(1)
print(f"MATCH: {len(co)} tickers identical — safe to proceed to the next cutover step")
PY
