# apogee CLI reference

This is the canonical reference for every `apogee` subcommand shipped in
v0.1.3. Every command is implemented under [`internal/cli/`](../internal/cli/)
and exposed through the cobra tree wired up in
[`internal/cli/root.go`](../internal/cli/root.go). Styling is handled by
[`charmbracelet/fang`](https://github.com/charmbracelet/fang), which renders
colourised `--help` output on a TTY and degrades cleanly to plain text when
piped.

```
Usage:
  apogee [command]

Available Commands:
  serve       Run the collector and embedded dashboard
  init        Install apogee hooks into .claude/settings.json
  hook        Forward a Claude Code hook payload to the apogee collector
  daemon      Install, start, stop, and inspect the background service
  status      One-shot daemon and HTTP liveness probe
  logs        Tail the daemon log files
  open        Open the dashboard in the default browser
  uninstall   Stop the daemon, strip hooks, optionally purge data
  menubar     Run the macOS menu bar app (macOS only)
  doctor      Health-check the local install
  version     Print build version information

Flags:
  -h, --help      help for apogee
  -v, --version   version for apogee
```

---

## apogee serve

Run the collector and embedded dashboard.

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `--addr` | `:4100` | HTTP listen address |
| `--db` | `~/.apogee/apogee.duckdb` | DuckDB database file |
| `--config` | `~/.apogee/config.toml` | TOML config file (optional) |

### Example

```sh
apogee serve --addr 127.0.0.1:4100 --db ~/.apogee/apogee.duckdb
```

### Notes

- On first run the collector creates the database file and applies migrations
  automatically.
- This is the exact command the `apogee daemon` supervisor installs into the
  launchd / systemd unit file; running it in the foreground is equivalent
  aside from logging to stdout/stderr instead of the daemon log files.

---

## apogee init

Install apogee hook entries into `.claude/settings.json`.

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `--scope` | `user` | Install scope: `user` (`~/.claude/settings.json`) or `project` (`./.claude/settings.json`) |
| `--target` | `` | Override target directory. Rarely needed. |
| `--server-url` | `http://localhost:4100/v1/events` | Collector endpoint written into every hook command |
| `--source-app` | `` | Pin the `source_app` label. Leave empty (default) to let the hook derive it at runtime. |
| `--dry-run` | `false` | Print the plan without writing |
| `--force` | `false` | Overwrite existing apogee hook entries without prompting. Also strips legacy `python3 send_event.py` rows from v0.1.x installs. |

### Example

```sh
# One install covers every Claude Code project on this machine.
apogee init

# Preview the changes without writing.
apogee init --dry-run

# Pin a fixed source_app instead of deriving per project.
apogee init --source-app my-team --force
```

### Notes

- Default scope is `user`. One install covers every Claude Code session on the
  machine, and the hook derives `source_app` at runtime from
  `$APOGEE_SOURCE_APP`, the git toplevel basename, or `$PWD` — see
  [`hooks.md`](hooks.md).
- The command written into `settings.json` is the absolute path of the
  currently-running `apogee` binary plus `hook --event <X> --server-url ...`.
  No Python is involved.
- If an older v0.1.x install left `python3 send_event.py` rows in place, the
  plan output warns about them and `--force` strips them.

---

## apogee hook

Forward a Claude Code hook payload to the apogee collector. This is the
command that `.claude/settings.json` points at for every hook event. The
binary itself is the hook; there is no separate hook script.

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `--event` | required | Hook event name (`PreToolUse`, `PostToolUse`, `UserPromptSubmit`, ...) |
| `--server-url` | `http://localhost:4100/v1/events` | Collector endpoint |
| `--source-app` | `` | Pin the source_app label. Empty = derive at runtime. |
| `--timeout` | `2s` | HTTP timeout for the events POST |

### Example

```sh
echo '{"session_id":"s-1","hook_event_type":"UserPromptSubmit"}' \
  | apogee hook --event UserPromptSubmit --server-url http://127.0.0.1:4100/v1/events
```

### Notes

- Reads a JSON hook payload from stdin, POSTs it to the collector, and echoes
  stdin back to stdout so the rest of the Claude Code hook pipeline is
  unaffected.
- On `PreToolUse` and `UserPromptSubmit` the hook first tries to claim an
  operator intervention via
  `POST /v1/sessions/{session_id}/interventions/claim`. On success, the
  Claude Code decision JSON is written to stdout in place of the stdin echo.
- Exit code is always 0 — a failing hook would break Claude Code.
- See [`hooks.md`](hooks.md) for the full wire contract.

---

## apogee daemon

Install, start, stop, and inspect apogee as a background service. macOS uses
launchd; Linux uses systemd `--user`.

### Subcommands

| Command | Description |
| --- | --- |
| `apogee daemon install` | Write the unit file and enable it |
| `apogee daemon uninstall` | Disable, remove the unit file |
| `apogee daemon start` | Launch the service now |
| `apogee daemon stop` | Stop the service |
| `apogee daemon restart` | Stop + start |
| `apogee daemon status` | Detailed install / running / PID / recent log report |

### Flags (on `install`)

| Flag | Default | Description |
| --- | --- | --- |
| `--addr` | `127.0.0.1:4100` | Listen address baked into the unit file |
| `--db` | `~/.apogee/apogee.duckdb` | DuckDB path baked into the unit file |
| `--force` | `false` | Overwrite an existing unit file |

### Example

```sh
apogee daemon install
apogee daemon start
apogee daemon status
apogee daemon restart
apogee daemon stop
apogee daemon uninstall
```

`apogee daemon status` prints two lipgloss boxes (Daemon + Collector),
captured here with `NO_COLOR=1`:

```
Daemon: dev.biwashi.apogee
╭─────────────────────────────────────────────────────────────────────────╮
│ Status:      running                                                    │
│ Installed:   yes                                                        │
│ Loaded:      yes                                                        │
│ Running:     yes                                                        │
│ PID:         12345                                                      │
│ Started at:  2026-04-15 13:01:20                                        │
│ Uptime:      1h 12m 4s                                                  │
│ Last exit:   0                                                          │
│ Unit path:   /Users/me/Library/LaunchAgents/dev.biwashi.apogee.plist    │
│ Logs:        ~/.apogee/logs/apogee.{out,err}.log                        │
╰─────────────────────────────────────────────────────────────────────────╯

Collector: http://127.0.0.1:4100
╭───────────────────────────────────────────────╮
│ Endpoint:  http://127.0.0.1:4100              │
│ Health:    ok                                 │
│ Detail:    ok (HTTP 200, 3 ms)                │
│ Latency:   3ms                                │
╰───────────────────────────────────────────────╯
```

### Notes

- The unit file lives at `~/Library/LaunchAgents/dev.biwashi.apogee.plist`
  on macOS and `~/.config/systemd/user/apogee.service` on Linux.
- The Collector box border turns red when the `/v1/healthz` probe fails;
  the Daemon box border turns yellow when not installed.
- See [`daemon.md`](daemon.md) for supervisor primitives, debugging, and
  configuration.

---

## apogee status

One-shot daemon and HTTP liveness probe. Prints a compact summary suitable
for shell prompts and CI checks.

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `--addr` | `127.0.0.1:4100` | Collector endpoint to probe |
| `--json` | `false` | Emit JSON instead of the default two-line text summary |

### Example

```
$ apogee status
APOGEE STATUS

Daemon:    running (pid 42317, uptime 1h 12m 4s)
╭─────────────────────────────────────────────────────────────────────────╮
│ Status:      running                                                    │
│ Installed:   yes                                                        │
│ Loaded:      yes                                                        │
│ Running:     yes                                                        │
│ PID:         42317                                                      │
│ Started at:  2026-04-15 13:01:20                                        │
│ Uptime:      1h 12m 4s                                                  │
│ Last exit:   0                                                          │
│ Unit path:   /Users/me/Library/LaunchAgents/dev.biwashi.apogee.plist    │
│ Logs:        ~/.apogee/logs/apogee.{out,err}.log                        │
╰─────────────────────────────────────────────────────────────────────────╯

Collector: http://127.0.0.1:4100 (ok (HTTP 200, 3 ms))
╭───────────────────────────────────────────────╮
│ Endpoint:  http://127.0.0.1:4100              │
│ Health:    ok                                 │
│ Detail:    ok (HTTP 200, 3 ms)                │
│ Latency:   3ms                                │
╰───────────────────────────────────────────────╯
```

### Notes

- Non-zero exit when the daemon is installed but not running, or when the
  HTTP probe fails. Useful as a login shell health check.

---

## apogee logs

Tail the daemon log files under `~/.apogee/logs/`.

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-f`, `--follow` | `false` | Follow new lines as they arrive |
| `-n`, `--lines` | `100` | Number of tail lines to show |
| `--err` | `false` | Follow the error log (`apogee.err.log`) instead of stdout |

### Example

```sh
apogee logs -f
apogee logs --err -n 200
```

### Notes

- When apogee is running in the foreground (`apogee serve`) logs go to the
  terminal directly and this command has nothing to tail.

---

## apogee open

Open the dashboard in the default browser.

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `--addr` | `127.0.0.1:4100` | Collector to open |

### Example

```sh
apogee open
```

### Notes

- On macOS shells out to `open`; on Linux tries `xdg-open`, then `open`.

---

## apogee uninstall

Stop the daemon, strip apogee hook entries from `.claude/settings.json`, and
optionally delete the data directory. Prompts before destructive actions.

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `--purge` | `false` | Also delete `~/.apogee/` (database + logs + config) |
| `--yes` | `false` | Do not prompt for confirmation |

### Example

```sh
apogee uninstall            # leaves data in place
apogee uninstall --purge    # also wipes ~/.apogee
```

### Notes

- Exits cleanly if the daemon is not installed or already stopped.
- The hooks stripper matches any entry whose command starts with the apogee
  binary path or the legacy `python3 send_event.py` prefix from v0.1.x.

---

## apogee menubar

Run the macOS menu bar app. macOS only — on other platforms the command
prints a clear "macOS only" message and exits non-zero.

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `--addr` | `127.0.0.1:4100` | Collector endpoint to poll |
| `--interval` | `5s` | Poll interval |

### Example

```sh
apogee menubar &
```

### Notes

- Requires the collector (or `apogee daemon start`) to be running — the menu
  bar is a client, not a server.
- The glyph shows a green dot for running turns, a red triangle for
  `intervene_now`, and "offline" when the collector is unreachable.
- See [`menubar.md`](menubar.md) for the full menu contents and
  troubleshooting.

---

## apogee doctor

Health-check the local install. Runs seven checks and prints a styled
glyph + message line for each, plus a summary footer.

### Checks

| Name | Description |
| --- | --- |
| `home` | `~/.apogee` exists and is writable |
| `claude_cli` | `claude` is on PATH (used by the summarizer) |
| `db_path` | Default DuckDB path is writable |
| `config` | `~/.apogee/config.toml` exists (informational) |
| `db_lock` | DuckDB sidecar lock is free, OR held by the installed daemon |
| `collector` | `GET /v1/healthz` on `127.0.0.1:4100` returns 200 (500 ms timeout) |
| `hook_install` | Every event in `internal/cli/init.go::HookEvents` is wired to the apogee binary in `~/.claude/settings.json` |

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `--json` | `false` | Emit the checks as a JSON array (suitable for CI / scripts / `apogee menubar`) |

### Example

```
$ apogee doctor

  ✓ /Users/me/.apogee writable
  ✓ claude CLI on PATH (/Users/me/.local/bin/claude)
  ✓ default db path /Users/me/.apogee/apogee.duckdb
  ✓ no config file (defaults in use) (/Users/me/.apogee/config.toml)
  ✓ DuckDB file is unlocked
  ⚠ collector not running (http://127.0.0.1:4100/v1/healthz)
  ✓ apogee hook installed for 12/12 events

5 ok · 1 warning · 0 errors
```

```
$ apogee doctor --json
[
  {"name": "home",         "severity": "ok",   "message": "/Users/me/.apogee writable"},
  {"name": "claude_cli",   "severity": "ok",   "message": "claude CLI on PATH", "detail": "/Users/me/.local/bin/claude"},
  {"name": "db_path",      "severity": "ok",   "message": "default db path /Users/me/.apogee/apogee.duckdb"},
  {"name": "config",       "severity": "info", "message": "no config file (defaults in use)", "detail": "/Users/me/.apogee/config.toml"},
  {"name": "db_lock",      "severity": "ok",   "message": "DuckDB file is unlocked"},
  {"name": "collector",    "severity": "warn", "message": "collector not running", "detail": "http://127.0.0.1:4100/v1/healthz"},
  {"name": "hook_install", "severity": "ok",   "message": "apogee hook installed for 12/12 events"}
]
```

### Notes

- `doctor` never modifies anything. It only reports.
- The glyphs are Unicode `✓` / `⚠` / `✗` (U+2713, U+26A0, U+2717). The
  output degrades cleanly when `NO_COLOR=1` is set or stdout is not a TTY.
- See [`doctor.md`](doctor.md) for the full check semantics.

---

## Common errors

### DuckDB lock conflict

When a second apogee process tries to open the same DuckDB file the second
invocation exits 1 with a styled error box instead of the raw driver error:

```
╭──────────────────────────────────────────────────────────╮
│ Another apogee process is already using the DuckDB file. │
│                                                          │
│ Path:    /Users/me/.apogee/apogee.duckdb                 │
│ Holder:  apogee (pid 12345)                              │
│                                                          │
│ To fix:                                                  │
│   1. apogee daemon stop                                  │
│   2. or: kill 12345                                      │
│   3. or: apogee serve --db <alt path>                    │
╰──────────────────────────────────────────────────────────╯
```

The holder PID is detected via `lsof -nP <db>` when available, with a
fallback to the sidecar pid file. See [`daemon.md`](daemon.md) for the
sidecar files (`<db>.apogee.lock`, `<db>.apogee.pid`).

### Daemon won't start

- Run `apogee daemon status` to see the install + load + running state.
- Run `apogee logs -f` to tail the daemon's stdout / stderr.
- On launchd: `launchctl print gui/$(id -u)/dev.biwashi.apogee`.
- On systemd: `journalctl --user -u apogee.service -f`.

### Hook not firing

Run `apogee doctor` and look at the `hook_install` row. If it reports
`partial` or `missing`, run `apogee init --force` to rewrite the entries.

---

## apogee version

Print build version, commit SHA, and build date.

### Flags

None.

### Example

```sh
$ apogee version
apogee 0.1.3 (commit abcdef1, built 2026-04-15)
```

### Notes

- `apogee --version` prints the short version string only; `apogee version`
  prints the full block above.

---

## Summarizer preferences

The LLM recap (Haiku) and rollup (Sonnet) workers honour a small set of
operator-controlled preferences persisted in the `user_preferences` DuckDB
table. They are read at the top of every job, so updates land without a
restart. Two ways to manage them:

1. The **Settings** page (`/settings`) has a "Summarizer" section with a
   language toggle, recap / rollup model overrides, and two system-prompt
   text areas. Save persists via `PATCH /v1/preferences`.
2. The compact **language picker** on the top ribbon flips
   `summarizer.language` between `EN` and `JA` in one click.

The same controls are exposed over HTTP for scripting:

```sh
# Read the current state.
curl -s http://localhost:4100/v1/preferences

# Switch the recap and rollup output to Japanese.
curl -s -X PATCH http://localhost:4100/v1/preferences \
  -H 'Content-Type: application/json' \
  -d '{"summarizer.language":"ja"}'

# Add a recap system prompt and override the recap model.
curl -s -X PATCH http://localhost:4100/v1/preferences \
  -H 'Content-Type: application/json' \
  -d '{"summarizer.recap_system_prompt":"Always mention the file paths.","summarizer.recap_model":"claude-haiku-4-5"}'

# Reset every summarizer.* preference back to the defaults.
curl -s -X DELETE http://localhost:4100/v1/preferences
```

Validation rules: `summarizer.language` must be `"en"` or `"ja"`, the two
system prompts are capped at 2048 characters each, and the model overrides
must look like a `claude-{haiku,sonnet,opus}-…` alias. Empty strings clear
the override and fall back to `~/.apogee/config.toml`.

---

## Global flags

These apply to every subcommand.

| Flag | Description |
| --- | --- |
| `-h`, `--help` | Print help for the command |
| `-v`, `--version` | Print `apogee --version` short string |

There is no global `--verbose` flag. Individual subcommands with network I/O
log their progress to stderr at INFO level and log errors regardless.

---

## Web routes

The embedded Next.js dashboard is served from the same origin as the `/v1`
API. Every route below is statically exported and accessible directly when
the collector is running (`apogee serve`). Trailing slashes are mandatory —
`output: "export"` writes one `index.html` per directory.

| Route | Purpose |
| --- | --- |
| `/` | Live dashboard. Focus card, triage rail, KPI strip, and the height-capped event ticker (PR #30). |
| `/sessions/` | Session catalog with search and source-app filter. |
| `/session/?id=<id>` | Tabbed session detail (Overview / Turns / Trace / Logs / Metrics). |
| `/turn/?sess=<sess>&turn=<turn>` | Turn detail with swim lane, recap, HITL, operator queue. |
| `/agents/` | Per-agent catalog (main + subagents). |
| `/insights/` | Aggregate analytics. |
| `/events/` | **PR #30** — paginated browser of every stored hook event. 50 rows per page, Prev / Next navigation, URL-backed `?page=N`, side-drawer JSON inspector. Backed by `GET /v1/events/recent`. |
| `/settings/` | Collector info and telemetry status. |
| `/styleguide/` | Design tokens and component reference. |
