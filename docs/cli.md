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

### Notes

- The unit file lives at `~/Library/LaunchAgents/dev.biwashi.apogee.plist`
  on macOS and `~/.config/systemd/user/apogee.service` on Linux.
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

```sh
$ apogee status
daemon:    running (pid 42317)
collector: ok (http://127.0.0.1:4100)
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

Health-check the local install. Verifies that the apogee binary resolves,
the config file parses, the database file is writable, `claude` is on PATH,
and a sample hook POST succeeds against the configured collector.

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `--server-url` | `http://localhost:4100/v1/events` | Collector endpoint to probe |

### Example

```sh
apogee doctor
```

### Notes

- `doctor` never modifies anything. It only reports.

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

## Global flags

These apply to every subcommand.

| Flag | Description |
| --- | --- |
| `-h`, `--help` | Print help for the command |
| `-v`, `--version` | Print `apogee --version` short string |

There is no global `--verbose` flag. Individual subcommands with network I/O
log their progress to stderr at INFO level and log errors regardless.
