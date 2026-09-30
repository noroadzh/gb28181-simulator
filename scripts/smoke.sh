#!/usr/bin/env bash
# scripts/smoke.sh — gb28181-simulator one-command smoke test.
#
# Change: fix-problems-and-smoke-deploy-docs, task 2.1.
# Purpose: Verify that the project builds, tests pass, cross-platform binaries
# are produced, the frontend bundles, and the server's critical HTTP/WS endpoints
# respond correctly.  All results are written to docs/smoke-results.json.
#
# Exit code: 0 = all passed, non-zero = first failed step exit code.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
RESULTS_FILE="$REPO_ROOT/docs/smoke-results.json"
BUILD_DIR="$REPO_ROOT/build"

# ── Helpers (cross-platform: Linux + macOS) ──────────────────────────────────

# Millisecond timestamps via Python (avoids Linux-specific date +%s%3N).
now_ms() {
  python3 -c 'import time; print(int(time.time() * 1000))'
}

sha256_file() {
  local f="$1"
  sha256sum "$f" > "${f}.sha256" 2>/dev/null \
    || shasum -a 256 "$f" > "${f}.sha256" 2>/dev/null \
    || true
}

write_results() {
  python3 - "$RESULTS_FILE" "$1" "$2" <<'PYEOF'
import json, sys
path, overall, total = sys.argv[1], sys.argv[2], int(sys.argv[3])
with open(path) as f:
    data = json.load(f)
data['overall'] = overall
data['total_duration_ms'] = total
with open(path, 'w') as f:
    json.dump(data, f, indent=2)
PYEOF
}

append_step() {
  python3 - "$RESULTS_FILE" "$1" "$2" "$3" "$4" "$5" <<'PYEOF'
import json, sys
path, name, status, dur = sys.argv[1:5]
artifacts = json.loads(sys.argv[5])
error = sys.argv[6]
if error == "":
        error = None
with open(path) as f:
        data = json.load(f)
data['steps'].append({
        "name": name,
        "status": status,
        "duration_ms": int(dur),
        "artifacts": artifacts,
        "error": error,
})
data['overall'] = 'passed' if all(s['status']=='passed' for s in data['steps']) else 'failed'
with open(path, 'w') as f:
        json.dump(data, f, indent=2)
PYEOF
}

echo "=== gb28181-simulator smoke test ==="
echo "Repo root: $REPO_ROOT"
echo "Results:   $RESULTS_FILE"
START_TOTAL=$(now_ms)

mkdir -p "$BUILD_DIR" "$REPO_ROOT/docs"

# Initial JSON skeleton
python3 - "$RESULTS_FILE" <<'PYEOF'
import json, sys
path = sys.argv[1]
import datetime
data = {
  "version": "1.0",
  "timestamp": datetime.datetime.utcnow().strftime("%Y-%m-%dT%H:%M:%SZ"),
  "overall": "unknown",
  "total_duration_ms": 0,
  "steps": [],
}
with open(path, 'w') as f:
  json.dump(data, f, indent=2)
PYEOF

OVERALL_RC=0
FAILED_STEP=""

# ── Step 1: Go unit tests ────────────────────────────────────────────────────
# NOTE: do not pass -race here. `-race` requires cgo, but the smoke job is
# explicitly a "production-shape" smoke that must build with CGO_ENABLED=0
# to match the release binaries. CI runners with Go ≥1.22 default to
# CGO_ENABLED=1, so the combination `CGO_ENABLED=0 go test -race` aborts
# with `go: -race requires cgo; enable cgo by setting CGO_ENABLED=1`.
# Race coverage belongs in `make test` / the lint+test jobs, not here.
echo ""
echo "--- [1] go-test ---"
T0=$(now_ms)
if (cd "$REPO_ROOT" && CGO_ENABLED=0 go test -count=1 ./...) 2>&1; then
  T1=$(now_ms); DUR=$((T1 - T0))
  echo "  ✓ go-test passed in ${DUR}ms"
  append_step "go-test" "passed" "$DUR" "[]" ""
else
  T1=$(now_ms); DUR=$((T1 - T0))
  OVERALL_RC=1; FAILED_STEP="go-test"
  echo "  ✗ go-test FAILED in ${DUR}ms"
  append_step "go-test" "failed" "$DUR" "[]" "go test returned non-zero"
fi

# ── Step 2: go vet ────────────────────────────────────────────────────────────
echo ""
echo "--- [2] go-vet ---"
T0=$(now_ms)
if (cd "$REPO_ROOT" && go vet ./...) 2>&1; then
  T1=$(now_ms); DUR=$((T1 - T0))
  echo "  ✓ go-vet passed in ${DUR}ms"
  append_step "go-vet" "passed" "$DUR" "[]" ""
else
  T1=$(now_ms); DUR=$((T1 - T0))
  OVERALL_RC=1; FAILED_STEP="go-vet"
  echo "  ✗ go-vet FAILED in ${DUR}ms"
  append_step "go-vet" "failed" "$DUR" "[]" "go vet returned non-zero"
fi

# ── Step 3: go build (host) ──────────────────────────────────────────────────
echo ""
echo "--- [3] go-build ---"
T0=$(now_ms)
if (cd "$REPO_ROOT" && CGO_ENABLED=0 go build ./...) 2>&1; then
  T1=$(now_ms); DUR=$((T1 - T0))
  echo "  ✓ go-build passed in ${DUR}ms"
  append_step "go-build" "passed" "$DUR" "[]" ""
else
  T1=$(now_ms); DUR=$((T1 - T0))
  OVERALL_RC=1; FAILED_STEP="go-build"
  echo "  ✗ go-build FAILED in ${DUR}ms"
  append_step "go-build" "failed" "$DUR" "[]" "go build returned non-zero"
fi

# ── Step 4: Cross-platform compilation ───────────────────────────────────────
echo ""
echo "--- [4] cross-compile ---"
T0=$(now_ms)
CROSS_FAILED=0
ARTIFACTS=()
for plat in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64; do
  OS=${plat%/*}
  ARCH=${plat#*/}
  EXT=""
  [ "$OS" = "windows" ] && EXT=".exe"
  if CGO_ENABLED=0 GOOS="$OS" GOARCH="$ARCH" go build \
    -trimpath -ldflags "-s -w" \
    -o "$BUILD_DIR/gb28181-simulator-$OS-$ARCH$EXT" \
    ./cmd/gb28181-simulator 2>&1; then
    echo "  ✓ $OS-$ARCH built"
    sha256_file "$BUILD_DIR/gb28181-simulator-$OS-$ARCH$EXT"
    ARTIFACTS+=("$BUILD_DIR/gb28181-simulator-$OS-$ARCH$EXT")
  else
    echo "  ✗ $OS-$ARCH FAILED"
    CROSS_FAILED=1
  fi
done
T1=$(now_ms); DUR=$((T1 - T0))
if [ $CROSS_FAILED -eq 0 ]; then
  echo "  ✓ cross-compile passed in ${DUR}ms"
  ARTIFACTS_JSON=$(python3 -c 'import json,sys; print(json.dumps(sys.argv[1:]))' "${ARTIFACTS[@]}")
  append_step "cross-compile" "passed" "$DUR" "$ARTIFACTS_JSON" ""
else
  OVERALL_RC=1; FAILED_STEP="cross-compile"
  echo "  ✗ cross-compile FAILED in ${DUR}ms"
  ARTIFACTS_JSON=$(python3 -c 'import json,sys; print(json.dumps(sys.argv[1:]))' "${ARTIFACTS[@]}")
  append_step "cross-compile" "failed" "$DUR" "$ARTIFACTS_JSON" "one or more platforms failed"
fi

# ── Step 5: Frontend build ────────────────────────────────────────────────────
echo ""
echo "--- [5] web-build ---"
T0=$(now_ms)
# Incremental install when node_modules exists (fast); cold `npm ci` otherwise.
WEB_INSTALL="npm ci --no-audit --no-fund"
[ -d "$REPO_ROOT/web/node_modules" ] && WEB_INSTALL="npm install --no-audit --no-fund"
if (cd "$REPO_ROOT/web" && eval "$WEB_INSTALL" && npm run build) 2>&1; then
  T1=$(now_ms); DUR=$((T1 - T0))
  echo "  ✓ web-build passed in ${DUR}ms"
  append_step "web-build" "passed" "$DUR" "[]" ""
else
  T1=$(now_ms); DUR=$((T1 - T0))
  OVERALL_RC=1; FAILED_STEP="web-build"
  echo "  ✗ web-build FAILED in ${DUR}ms"
  append_step "web-build" "failed" "$DUR" "[]" "npm ci or npm run build failed"
fi

# ── Step 6: e2e smoke ─────────────────────────────────────────────────────────
echo ""
echo "--- [6] e2e-smoke ---"
T0=$(now_ms)
if (cd "$REPO_ROOT" && go test -v -count=1 -timeout=120s ./internal/test/e2e/...) 2>&1; then
  T1=$(now_ms); DUR=$((T1 - T0))
  echo "  ✓ e2e-smoke passed in ${DUR}ms"
  append_step "e2e-smoke" "passed" "$DUR" "[]" ""
else
  T1=$(now_ms); DUR=$((T1 - T0))
  OVERALL_RC=1; FAILED_STEP="e2e-smoke"
  echo "  ✗ e2e-smoke FAILED in ${DUR}ms"
  append_step "e2e-smoke" "failed" "$DUR" "[]" "go test ./internal/test/e2e/... returned non-zero"
fi

# ── Finalize ──────────────────────────────────────────────────────────────────
END_TOTAL=$(now_ms); TOTAL_DUR=$((END_TOTAL - START_TOTAL))
OVERALL_STR="passed"
[ $OVERALL_RC -ne 0 ] && OVERALL_STR="failed"
write_results "$OVERALL_STR" "$TOTAL_DUR"

echo ""
echo "=== Smoke complete ==="
echo "Overall: $OVERALL_STR"
[ -n "$FAILED_STEP" ] && echo "Failed step: $FAILED_STEP"
echo "Duration: ${TOTAL_DUR}ms"
echo "Results:  $RESULTS_FILE"
echo "Builds:   $BUILD_DIR/"

exit $OVERALL_RC