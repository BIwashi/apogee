#!/usr/bin/env bash
# capture-screenshots.sh — boot the apogee collector against an in-memory
# DuckDB, post a realistic fixture batch, drive Chromium via playwright to
# capture four dashboard PNGs, and tear everything down.
#
# Usage: bash scripts/capture-screenshots.sh
# Requires: bash, curl, node 20+, the claude CLI is NOT required.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SCRIPTS_DIR="$REPO_ROOT/scripts"
OUT_DIR="$REPO_ROOT/assets/screenshots"
BIN="$REPO_ROOT/bin/apogee"
PORT=4977
BASE="http://127.0.0.1:${PORT}"

mkdir -p "$OUT_DIR"

if [ ! -x "$BIN" ]; then
  echo "[capture] building apogee binary…"
  (cd "$REPO_ROOT" && make build)
fi

# Boot the collector against an in-memory DB so we leave no on-disk state.
echo "[capture] starting collector on :$PORT"
APOGEE_LOG_LEVEL=error \
  "$BIN" serve --addr ":$PORT" --db ":memory:" --log-level error \
  >/tmp/apogee-screenshots.log 2>&1 &
PID=$!

cleanup() {
  if kill -0 "$PID" 2>/dev/null; then
    kill "$PID" 2>/dev/null || true
    wait "$PID" 2>/dev/null || true
  fi
}
trap cleanup EXIT INT TERM

# Wait for /v1/healthz to return 200.
echo "[capture] waiting for healthz…"
for i in $(seq 1 50); do
  if curl -fsS "$BASE/v1/healthz" >/dev/null 2>&1; then
    break
  fi
  sleep 0.2
  if [ "$i" = "50" ]; then
    echo "[capture] timed out waiting for collector"
    exit 1
  fi
done

# Post the screenshot fixtures so the dashboard has lived-in data.
echo "[capture] posting fixture batch"
curl -fsS -H 'Content-Type: application/json' \
  -X POST "$BASE/v1/events" \
  --data-binary "@$SCRIPTS_DIR/screenshot_fixtures.json" >/dev/null

# Give the reconstructor + attention engine a beat to settle.
sleep 0.5

# Install playwright into scripts/ on first run. node_modules is gitignored.
if [ ! -d "$SCRIPTS_DIR/node_modules/playwright" ]; then
  echo "[capture] installing playwright into scripts/ (first run)"
  (cd "$SCRIPTS_DIR" && npm install --silent --no-audit --no-fund)
  (cd "$SCRIPTS_DIR" && npx playwright install chromium >/dev/null)
fi

echo "[capture] running playwright capture"
OUT_DIR="$OUT_DIR" APOGEE_BASE_URL="$BASE" \
  node "$SCRIPTS_DIR/capture.mjs"

echo "[capture] done. screenshots in $OUT_DIR"
ls -lh "$OUT_DIR"
