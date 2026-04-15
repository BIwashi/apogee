# apogee hook contract

The Claude Code hook entry point is the `apogee` binary itself. There is no
separate hook script, no Python runtime, no embedded hooks filesystem, and no
`hooks/` directory to extract. `apogee init` writes the absolute path of the
currently-running binary plus `hook --event <X> --server-url ...` into
`.claude/settings.json`, and that is the entire install.

This document describes the end-to-end contract: what Claude Code passes on
stdin, what apogee writes to stdout, the wire shape of the events POST, the
intervention claim flow, and how to debug a hook that is not firing.

---

## Wire contract

Claude Code hooks are JSON-over-stdio. For every hook event, Claude Code:

1. Looks up the `hooks.<event>` array in `.claude/settings.json`.
2. For each entry, executes the `command` with the hook payload on stdin as
   a single JSON object.
3. Reads stdout. If the stdout JSON sets `decision: "block"` (on
   `PreToolUse`) or contains a `hookSpecificOutput.additionalContext` field
   (on `UserPromptSubmit`), Claude Code honours it.
4. A non-zero exit code fails the hook and, depending on the event, may
   block the agent.

apogee follows the contract strictly. Every `apogee hook` invocation:

1. Reads the full hook payload from stdin.
2. (PreToolUse / UserPromptSubmit only) Tries to claim an operator
   intervention via `POST /v1/sessions/<session_id>/interventions/claim`.
3. Writes either the original stdin payload (pass-through) or a Claude Code
   decision JSON (claimed intervention) to stdout.
4. POSTs the full hook telemetry to `/v1/events`.
5. Exits 0 unconditionally. Transport failures log to stderr and are
   swallowed — a failing hook would break Claude Code, which is the exact
   opposite of what an observability tool should do.

The implementation lives in
[`internal/cli/hook.go`](../internal/cli/hook.go) with tests alongside in
[`internal/cli/hook_test.go`](../internal/cli/hook_test.go).

---

## Install via `apogee init`

```sh
apogee init                # default: user scope (~/.claude/settings.json)
apogee init --scope project
apogee init --dry-run
apogee init --force        # also strips legacy python3 send_event.py rows
```

After `apogee init` the hooks section of `settings.json` looks like:

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "/usr/local/bin/apogee hook --event PreToolUse --server-url http://localhost:4100/v1/events"
          }
        ]
      }
    ],
    "PostToolUse": [
      { "hooks": [ { "type": "command", "command": "/usr/local/bin/apogee hook --event PostToolUse --server-url http://localhost:4100/v1/events" } ] }
    ],
    ...
  }
}
```

The absolute binary path is the output of `os.Executable()` at init time, so
running `apogee init` from a brew install points at `/opt/homebrew/bin/apogee`
and running it from `./bin/apogee` during local development points at that
dev binary. This is how contributors run their local collector against their
own Claude Code sessions without touching the brew install.

Default scope is `user`. One install on a machine covers every Claude Code
project, and the hook derives `source_app` at runtime so each project still
lights up with the right label.

---

## Supported hook events

The canonical list lives in
[`internal/cli/init.go::HookEvents`](../internal/cli/init.go) and is:

| Event | Purpose |
| --- | --- |
| `SessionStart` | new Claude Code session opened |
| `SessionEnd` | session ended / terminated |
| `UserPromptSubmit` | user sent a prompt → opens a turn root span |
| `PreToolUse` | about to call a tool → opens a tool span |
| `PostToolUse` | tool call succeeded → closes the tool span |
| `PostToolUseFailure` | tool call failed → closes with error status |
| `PermissionRequest` | HITL permission request surfaced to the operator |
| `Notification` | Claude Code notification surfaced during a turn |
| `SubagentStart` | subagent spawned → opens a subagent span |
| `SubagentStop` | subagent reclaimed → closes the subagent span |
| `Stop` | turn reached end of turn → closes the turn root |
| `PreCompact` | context window about to be compacted |

`apogee init` writes one entry per event in the order above.

---

## Dynamic `source_app` derivation

`apogee hook --event X` derives a `source_app` label at invocation time from,
in order:

1. `$APOGEE_SOURCE_APP` — explicit override.
2. `basename $(git rev-parse --show-toplevel)` — the repo name, when the
   session is inside a git repository.
3. `basename $PWD` — the directory name otherwise.
4. Literal `"unknown"` — last-resort fallback.

This is why the default `apogee init` does NOT pin `--source-app`. One
install at user scope automatically labels every project with its own repo
name: starting `claude` in `~/work/newmo-backend` labels every event
`source_app=newmo-backend`, and starting `claude` in `~/work/apogee` labels
every event `source_app=apogee`, with zero per-project configuration.

Pinning a fixed label is still supported: `apogee init --source-app foo`
writes `--source-app foo` into every hook command and the runtime
derivation is skipped.

The derivation helper is `deriveSourceAppRuntime` in
[`internal/cli/hook.go`](../internal/cli/hook.go).

---

## Intervention claim flow

`PreToolUse` and `UserPromptSubmit` carry operator-initiated interventions.
Before POSTing telemetry, the hook:

1. Extracts `session_id` (and optional `turn_id`) from the stdin payload.
2. POSTs `{"hook_event": "<event>", "turn_id": "..."}` to
   `POST /v1/sessions/<session_id>/interventions/claim` with a 1.5s budget.
3. On `204 No Content` the hook stays in pass-through mode — echo stdin to
   stdout and POST the hook event normally.
4. On `200 OK` with a claimed row in the body, the hook inspects the
   `delivery_mode`:
   - `interrupt` on `PreToolUse` → stdout receives
     `{"decision":"block","reason":"<message>"}`.
   - `context` on `UserPromptSubmit` → stdout receives
     `{"hookSpecificOutput":{"additionalContext":"<message>"}}`.
   - `both` → whichever hook fires first wins; `PreToolUse` is preferred.
   If the claimed mode does not match the hook (e.g. `context` on
   `PreToolUse`), the hook logs and pass-throughs — the sweeper will expire
   the row.
5. Fires a best-effort `POST /v1/interventions/<id>/delivered` so the
   collector flips the row to `delivered` and broadcasts
   `intervention.delivered` over SSE.

See [`interventions.md`](interventions.md) for the full lifecycle and REST
surface, and the `maybeClaimIntervention` / `decisionForMode` / `markDelivered`
helpers in [`internal/cli/hook.go`](../internal/cli/hook.go).

---

## Wire shape — `POST /v1/events`

Every hook event produces one POST. The body is JSON and the shape matches
[`internal/ingest/payload.go::HookEvent`](../internal/ingest/payload.go):

```json
{
  "source_app": "newmo-backend",
  "session_id": "sess-01HXYZ...",
  "hook_event_type": "PreToolUse",
  "timestamp": 1713138123456,

  "tool_name": "Bash",
  "tool_use_id": "tool_01HXYZ...",
  "payload": { ... verbatim stdin JSON ... }
}
```

Top-level fields are always present. Every field inside `flatHookFields`
(`tool_name`, `tool_use_id`, `error`, `is_interrupt`, `agent_id`,
`agent_type`, `stop_hook_active`, `notification_type`, `custom_instructions`,
`source`, `reason`, `model_name`, `prompt`, ...) is promoted from the stdin
payload to the top level when present so the collector can read them without
touching `payload`. The raw stdin bytes are preserved verbatim under
`payload` so the reconstructor sees the exact bytes Claude Code emitted.

Content type is `application/json`, user agent is
`apogee-hook/<build-version>`.

---

## Debugging

### 1. Is the hook firing at all?

Run any quick Claude Code action (a tool call is easiest — e.g. `ls` in the
REPL) and then check the `hook_event` column on the most recent logs:

```sh
curl -s 'http://127.0.0.1:4100/v1/sessions/recent' | jq '.[0:3]'
```

If the response is empty, Claude Code never fired a hook. Double-check
`.claude/settings.json`:

```sh
jq '.hooks | keys' ~/.claude/settings.json
```

All 12 events listed under [Supported hook events](#supported-hook-events)
should be present.

### 2. Is the collector reachable?

Run the hook manually with an empty payload:

```sh
echo '{"session_id":"s-debug","hook_event_type":"UserPromptSubmit"}' \
  | apogee hook --event UserPromptSubmit --server-url http://127.0.0.1:4100/v1/events
```

The stdout should echo the input (or a decision JSON if an intervention
matched), and stderr should be empty. Any stderr line starting with
`apogee hook:` is a transport failure logged by the hook — the exit code is
still 0, but the POST did not land.

Probe the collector directly:

```sh
curl -s http://127.0.0.1:4100/v1/healthz
```

### 3. Is the event being reconstructed?

Look for the corresponding span by tool name:

```sh
curl -s 'http://127.0.0.1:4100/v1/turns/active' | jq '.[0].turn_id'
```

Then fetch the span tree for that turn:

```sh
curl -s "http://127.0.0.1:4100/v1/turns/<turn_id>/spans" | jq .
```

If the hook fired and the collector received it but no span appears, the
reconstructor hit a validation error. Check the server logs for
`reconstructor: ...` entries.

### 4. Is the intervention claim working?

On a running turn, submit an intervention via `POST /v1/interventions` and
then watch the next hook:

```sh
curl -s http://127.0.0.1:4100/v1/interventions \
  -H 'Content-Type: application/json' \
  -d '{"session_id":"<sess>","message":"Stop and reconsider","delivery_mode":"interrupt","scope":"this_turn","urgency":"high"}'
```

The next `PreToolUse` hook should claim it, stdout should carry the
`{"decision":"block","reason":"Stop and reconsider"}` JSON, and the row
should transition to `delivered` in the dashboard.

---

## Contract invariants

- **Exit code is always 0.** Every failure path logs to stderr and
  `return nil`s from `runHook`.
- **Stdin is echoed verbatim** unless a claimed intervention replaces it.
  The claim is the only reason stdout ever differs from stdin.
- **Telemetry POST is best-effort.** A collector outage does not block the
  hook; the claim step times out after 1.5s and the events POST after the
  `--timeout` value (default 2s). Both are measured per-request.
- **No side effects on disk.** The hook never writes files. The only
  persistent state lives on the collector, the Claude Code hook payload on
  stdin, and the `stderr` stream.
