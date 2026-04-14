#!/usr/bin/env bash
# smoke_test.sh — end-to-end verification of the apogee hook path.
#
# Starts a short-lived in-memory collector, runs `apogee init`, emits a fake
# hook event through `send_event.py`, and curls the collector to verify the
# event landed. Not wired into CI — run it manually when changing the hook
# library or the init command.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="${REPO_ROOT}/bin/apogee"
TEST_DIR="$(mktemp -d -t apogee-smoke-XXXXXX)"
COLLECTOR_PID=""

cleanup() {
    if [[ -n "${COLLECTOR_PID}" ]] && kill -0 "${COLLECTOR_PID}" 2>/dev/null; then
        kill "${COLLECTOR_PID}" 2>/dev/null || true
        wait "${COLLECTOR_PID}" 2>/dev/null || true
    fi
    rm -rf "${TEST_DIR}"
}
trap cleanup EXIT

if [[ ! -x "${BIN}" ]]; then
    echo "smoke_test: building apogee binary"
    (cd "${REPO_ROOT}" && go build -o ./bin/apogee ./cmd/apogee)
fi

echo "smoke_test: starting collector"
"${BIN}" serve -addr :4100 -db :memory: >"${TEST_DIR}/collector.log" 2>&1 &
COLLECTOR_PID=$!

# Wait for the collector to bind.
for _ in 1 2 3 4 5 6 7 8 9 10; do
    if curl -fsS http://localhost:4100/healthz >/dev/null 2>&1; then
        break
    fi
    sleep 0.2
done

echo "smoke_test: dry run"
"${BIN}" init \
    --target "${TEST_DIR}/.claude" \
    --source-app smoke-test \
    --dry-run

echo "smoke_test: real install"
"${BIN}" init \
    --target "${TEST_DIR}/.claude" \
    --source-app smoke-test

SEND_EVENT="$(grep -oE 'python3 [^[:space:]]+send_event\.py' "${TEST_DIR}/.claude/settings.json" | head -n1 | awk '{print $2}')"
if [[ -z "${SEND_EVENT}" ]]; then
    echo "smoke_test: failed to locate send_event.py in settings.json" >&2
    exit 1
fi

PAYLOAD='{"session_id":"smoke-sess","tool_name":"Bash","tool_use_id":"tu-smoke-1","command":"ls"}'
echo "${PAYLOAD}" | python3 "${SEND_EVENT}" \
    --source-app smoke-test \
    --event-type PreToolUse \
    --server-url http://localhost:4100/v1/events

# Give the collector a moment to commit.
sleep 0.3

echo "smoke_test: verifying event landed"
RESPONSE="$(curl -fsS http://localhost:4100/v1/turns/recent)"
echo "${RESPONSE}" | python3 -c 'import json,sys; d=json.loads(sys.stdin.read()); print(json.dumps(d, indent=2))'

echo "smoke_test: ok"
