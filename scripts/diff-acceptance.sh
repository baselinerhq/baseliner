#!/usr/bin/env bash
# Differential acceptance: the Go port and the legacy Python tool must produce
# identical per-repo scores and pass/fail verdicts for the same config.
#
# Usage: scripts/diff-acceptance.sh <baseliner.yaml> [python-repo-dir]
# Requires: go, uv, python3 (+ GITHUB_TOKEN in env for github-scope configs).
set -euo pipefail

CONFIG="${1:?usage: diff-acceptance.sh <baseliner.yaml> [python-repo-dir]}"
PYDIR="${2:-../baseliner}"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
GOBIN="$TMP/baseliner-go"
GOJSON="$TMP/go.json"
PYJSON="$TMP/py.json"

echo "building go binary..."
go build -o "$GOBIN" ./cmd/baseliner

echo "running go scan..."
"$GOBIN" scan --config "$CONFIG" --format json --output-file "$GOJSON" 2>/dev/null || true
echo "running python scan..."
( cd "$PYDIR" && uv run baseliner scan --config "$CONFIG" --format json --output-file "$PYJSON" 2>/dev/null ) || true

python3 - "$GOJSON" "$PYJSON" <<'PY'
import json, sys
def norm(p):
    d = json.load(open(p))
    return {r["slug"].split("/")[-1]: (round(r["score"], 4),
            any(c["status"] in ("fail", "error") for c in r["results"]))
            for r in d["repos"]}
go, py = norm(sys.argv[1]), norm(sys.argv[2])
keys = sorted(set(go) | set(py))
mism = [k for k in keys if go.get(k) != py.get(k)]
print(f"{'repo':<28}{'python':>18}{'go':>18}  match")
for k in keys:
    ok = go.get(k) == py.get(k)
    print(f"{k:<28}{str(py.get(k)):>18}{str(go.get(k)):>18}  {'OK' if ok else 'MISMATCH'}")
print(f"\n{len(keys)} repos | {len(mism)} mismatches | "
      + ("DIFFERENTIAL: PASS" if not mism else "DIFFERENTIAL: FAIL"))
sys.exit(1 if mism else 0)
PY
