# apogee hooks

A stdlib-only Python hook library that forwards Claude Code hook events to the
apogee collector. Wire-compatible with the reference hooks from
[disler/claude-code-hooks-multi-agent-observability](https://github.com/disler/claude-code-hooks-multi-agent-observability).

## Files

| File | Purpose |
|---|---|
| `apogee_hook.py` | Shared library. Exports `send_event`, `read_hook_input`, `extract_top_level_fields`, `build_event`. |
| `send_event.py` | CLI entry point called from `.claude/settings.json`. Reads stdin, forwards to the collector, echoes stdin to stdout. |
| `install.py` | Standalone installer that edits `.claude/settings.json`. `apogee init` is the preferred path; this is a bootstrap fallback. |
| `example_settings.json` | Reference `.claude/settings.json` fragment for all 12 hook events. |
| `tests/test_apogee_hook.py` | Stdlib-only unittest suite. |
| `run_tests.sh` | `python3 -m unittest discover hooks/tests`. |
| `smoke_test.sh` | End-to-end test (collector → init → send event → curl). |

## Install with `apogee init`

```sh
cd your/project
apogee serve &               # start the collector on :4100
apogee init                  # writes ./.claude/settings.json
```

`apogee init` extracts the embedded hook scripts to
`~/.apogee/hooks/<version>/` and points `.claude/settings.json` at them, so a
future `apogee` upgrade re-extracts automatically on the next `init`.

### Flags

```
apogee init [flags]
  --target <path>        Claude Code settings directory (default: ~/.claude for user scope)
  --source-app <name>    Pin the source_app label. Leave empty for dynamic derivation.
  --server-url <url>     Collector URL (default: http://localhost:4100/v1/events)
  --scope <user|project> Install to ~/.claude/ (user) or ./.claude/ (project). Default: user.
  --dry-run              Print plan without writing
  --force                Overwrite existing apogee hook entries
```

Dynamic `source_app` resolution order (used when `--source-app` is omitted):

1. `$APOGEE_SOURCE_APP` environment variable
2. `basename $(git rev-parse --show-toplevel)`
3. `basename $PWD`
4. `"unknown"` as the last-resort fallback

One user-scope install on a machine therefore reports every project under its own repo name with no per-project reconfiguration.

## Install with `install.py` (no Go binary)

```sh
python3 hooks/install.py \
    --target ./.claude \
    --source-app my-project \
    --server-url http://localhost:4100/v1/events
```

## Hooked events

All 12 Claude Code hook events are installed:

- `SessionStart`, `SessionEnd`
- `UserPromptSubmit`
- `PreToolUse`, `PostToolUse`, `PostToolUseFailure`
- `PermissionRequest`, `Notification`
- `SubagentStart`, `SubagentStop`
- `Stop`, `PreCompact`

## Wire contract

Each hook event is serialised into the apogee `HookEvent` shape (see
`internal/ingest/payload.go`):

```json
{
  "source_app": "my-project",
  "session_id": "sess-alpha",
  "hook_event_type": "PreToolUse",
  "timestamp": 1735689602000,
  "tool_name": "Bash",
  "tool_use_id": "tu-bash-1",
  "payload": { ... original hook input ... }
}
```

Fields listed at the top level of `HookEvent` are also promoted out of
`payload` for convenient filtering. The collector tolerates both flattened and
non-flattened variants.

## Failure behaviour

Hook scripts must never break Claude Code. If the collector is unreachable,
the hook logs to stderr and exits 0. The original stdin is always echoed back
to stdout so the rest of the Claude Code pipeline is unaffected.

## Requirements

- Python 3.10+ (3.11+ preferred). No third-party dependencies.
