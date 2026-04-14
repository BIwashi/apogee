#!/bin/sh
# Minimal stand-in for the `claude` CLI used by runner_test.go. Reads the
# prompt from stdin, discards the flags, and emits a claude-shaped JSON
# envelope where `result` is a canned Recap JSON payload.
cat > /dev/null
cat <<'JSON'
{"type":"result","subtype":"success","is_error":false,"num_turns":1,"session_id":"fake","result":"{\n  \"headline\": \"fake recap\",\n  \"outcome\": \"success\",\n  \"phases\": [],\n  \"key_steps\": [\"one\", \"two\", \"three\"],\n  \"failure_cause\": null,\n  \"notable_events\": []\n}"}
JSON
