# `apogee doctor` reference

`apogee doctor` is the local-install health check. It runs seven probes
against the host environment, the DuckDB store, and the Claude Code hook
config, then prints a one-line styled glyph summary for each plus a
final tally. The command is read-only and never modifies anything.

A `--json` flag emits the same checks as a JSON array suitable for CI,
shell scripts, or the macOS menu bar app.

## Synopsis

```sh
apogee doctor          # styled text output
apogee doctor --json   # machine-readable JSON
```

## Checks

Every check produces a single `DoctorCheck` row with these fields:

| Field | Meaning |
| --- | --- |
| `name` | Stable identifier — one of the seven listed below |
| `severity` | `ok` / `warn` / `error` / `info` |
| `message` | Short human-readable summary |
| `detail` | Optional supporting context (path, URL, error string) |

### 1. `home` — `~/.apogee` writable

Resolves the user's home directory, joins `.apogee`, and probes
write access by creating + removing a temporary file. Severity is
`ok` when the directory is writable, `warn` otherwise (e.g. when the
filesystem is read-only or the home directory cannot be resolved).

### 2. `claude_cli` — `claude` on PATH

Looks up `claude` via `exec.LookPath`. The summarizer relies on the
local Claude Code CLI (Haiku for per-turn recaps, Sonnet for per-session
rollups), so a missing binary is reported as `warn` — the collector
still runs, but the summarizer is disabled.

### 3. `db_path` — default DuckDB path writable

Joins `~/.apogee/apogee.duckdb`, then checks the parent directory is
writable. Reports the resolved path so users know exactly where the
collector will write the database. `warn` when the directory does not
exist or is not writable.

### 4. `config` — `~/.apogee/config.toml`

Stats `~/.apogee/config.toml`. Reports `ok` when the file is present
(daemon, summarizer, telemetry knobs are loaded from it), or `info`
when it is absent (defaults apply). Never an error — the collector
boots happily without a config file.

### 5. `db_lock` — DuckDB sidecar lock

Calls `duckdb.CheckDBLockHolder` against the configured DuckDB path:

- **OK** — the sidecar lock is free; nothing has the database open.
- **OK** — the lock is held but the holder PID matches the PID of the
  installed daemon (the expected good state when you have run
  `apogee daemon start`). The check looks the daemon up via the same
  `managerFactory` the daemon subcommands use, then compares the
  daemon `Status.PID` against the lock holder PID.
- **error** — the lock is held by some other process. The check
  reports the holder PID and points at the styled error box the
  collector itself would print on a conflicting `serve`.

The probe touches a sidecar `.apogee.lock` file, never the DuckDB
file itself, so it cannot conflict with the live collector when the
daemon is running. See [`daemon.md`](daemon.md#duckdb-lock) for the
sidecar files and the conflict box.

### 6. `collector` — `/v1/healthz` reachability

Issues `GET http://127.0.0.1:4100/v1/healthz` with a 500 ms timeout.

- **OK** — HTTP 200.
- **warn** — connection refused / timeout. The collector is not
  running, which is expected when you have not yet started the
  daemon or the foreground `apogee serve`.
- **error** — non-200 status. Means the collector is up but
  misbehaving.

### 7. `hook_install` — `.claude/settings.json` coverage

Reads `~/.claude/settings.json` and verifies every event in
`internal/cli/init.go::HookEvents` (12 events: SessionStart, SessionEnd,
UserPromptSubmit, PreToolUse, PostToolUse, PostToolUseFailure,
PermissionRequest, Notification, SubagentStart, SubagentStop, Stop,
PreCompact) has at least one hook entry whose command contains the
literal `"apogee hook"` (or `"apogee-hook"` for legacy installs).

- **OK** — all 12 events are covered.
- **warn** (`partial`) — some events are covered, others are missing.
  The detail line lists the missing event names.
- **warn** (`missing`) — no apogee entries at all. Run `apogee init`
  to install them.

The check is intentionally lenient: a partial install is `warn` not
`error`, so users on weird shells / multi-hosted home directories
don't get scary red output for a state apogee can still recover from
with `apogee init --force`.

## Sample output

### Default (styled text)

```
$ apogee doctor

apogee doctor

  ✓ /Users/me/.apogee writable
  ✓ claude CLI on PATH (/Users/me/.local/bin/claude)
  ✓ default db path /Users/me/.apogee/apogee.duckdb
  ✓ no config file (defaults in use) (/Users/me/.apogee/config.toml)
  ✓ DuckDB file is unlocked
  ⚠ collector not running (http://127.0.0.1:4100/v1/healthz)
  ✓ apogee hook installed for 12/12 events

5 ok · 1 warning · 0 errors
```

The glyphs are Unicode `✓` / `⚠` / `✗` (U+2713, U+26A0, U+2717). They
are NOT emoji — they pass the design-token `no-emoji` rule. The
output degrades cleanly to plain ASCII text when `NO_COLOR=1` is set
or stdout is not a TTY (lipgloss + colorprofile handle this; nothing
in `doctor.go` writes raw ANSI).

### `--json`

```
$ apogee doctor --json
[
  {
    "name": "home",
    "severity": "ok",
    "message": "/Users/me/.apogee writable"
  },
  {
    "name": "claude_cli",
    "severity": "ok",
    "message": "claude CLI on PATH",
    "detail": "/Users/me/.local/bin/claude"
  },
  {
    "name": "db_path",
    "severity": "ok",
    "message": "default db path /Users/me/.apogee/apogee.duckdb"
  },
  {
    "name": "config",
    "severity": "info",
    "message": "no config file (defaults in use)",
    "detail": "/Users/me/.apogee/config.toml"
  },
  {
    "name": "db_lock",
    "severity": "ok",
    "message": "DuckDB file is unlocked"
  },
  {
    "name": "collector",
    "severity": "warn",
    "message": "collector not running",
    "detail": "http://127.0.0.1:4100/v1/healthz"
  },
  {
    "name": "hook_install",
    "severity": "ok",
    "message": "apogee hook installed for 12/12 events"
  }
]
```

## Exit code

`apogee doctor` always exits 0 — even when checks fail. The intent is
to make it safe to call from CI / health probes without pinging an
alert pipeline for a transient warning. Scripts that need to gate on
specific severities should consume `--json` and read the `severity`
field per check.
