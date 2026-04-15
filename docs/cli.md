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
  onboard     Interactive setup wizard (hooks + daemon + summarizer + dashboard)
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

## apogee onboard

One-command interactive setup. Walks a new machine through the four install
steps — hooks, daemon, summarizer preferences, OTLP export — and starts the
daemon + opens the dashboard at the end. Every prompt's default is loaded
from the live state on disk, so re-runs are safe and only propose the
deltas you actually want.

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `--yes`, `-y` | `false` | Accept every default without prompting |
| `--non-interactive` | `false` | Alias for `--yes`, clearer in scripts |
| `--config` | `~/.apogee/config.toml` | Config file to write |
| `--db` | `~/.apogee/apogee.duckdb` | DuckDB file for preferences |
| `--addr` | `127.0.0.1:4100` | Collector bind address |
| `--skip-daemon` | `false` | Do not install / start the daemon |
| `--skip-hooks` | `false` | Do not install hooks |
| `--skip-summarizer` | `false` | Do not write summarizer preferences |
| `--skip-telemetry` | `false` | Do not configure OTLP export |
| `--dry-run` | `false` | Show the plan without writing anything |

### Example

```sh
# Fully interactive: walk through every section.
apogee onboard

# CI / docker provisioning: accept every default silently.
apogee onboard --yes

# Preview the plan without touching disk.
apogee onboard --dry-run
```

### Notes

- `APOGEE_ONBOARD_NONINTERACTIVE=1` is equivalent to `--yes`, useful inside
  Docker `RUN` steps.
- Non-interactive mode never overwrites a non-empty existing system prompt
  with an empty default.
- The wizard drives the same package-level helpers that `apogee init` and
  `apogee daemon install` use, so every step is idempotent.
- First failing step aborts with a styled error box; earlier successes are
  **not** rolled back.
- See [`onboard.md`](onboard.md) for the full walkthrough, plan format, and
  failure semantics.

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

The three LLM tiers — recap (Haiku, per turn), rollup (Sonnet, per
session), and narrative (Sonnet, per session) — honour a small set of
operator-controlled preferences persisted in the `user_preferences` DuckDB
table. They are read at the top of every job, so updates land without a
restart. Two ways to manage them:

1. The **Settings** page (`/settings`) has a "Summarizer" section with a
   language toggle, recap / rollup / narrative model overrides, and three
   system-prompt text areas. Save persists via `PATCH /v1/preferences`.
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

Validation rules: `summarizer.language` must be `"en"` or `"ja"`, the
three system prompts are capped at 2048 characters each, and the model
overrides must match an entry in the static catalog exposed by
`GET /v1/models` (see below). Empty strings clear the override and fall
back to the catalog resolver.

### Summarizer model catalog

apogee ships a curated static catalog of known Claude models in
[`internal/summarizer/models.go`](../internal/summarizer/models.go). The
catalog is the single source of truth for every UI dropdown, the
onboard wizard, and the `summarizer.*_model` validation path. When
Anthropic ships a new model, add an entry and ship a new apogee release.

The catalog currently carries:

| Alias | Display | Status | Tier | Recommended for |
| --- | --- | --- | --- | --- |
| `claude-haiku-4-5` | Haiku 4.5 | current | 0 (cheapest) | recap |
| `claude-sonnet-4-6` | Sonnet 4.6 | current | 1 | recap / rollup / narrative |
| `claude-opus-4-6` | Opus 4.6 | current | 2 | rollup / narrative |
| `claude-haiku-3-5` | Haiku 3.5 | legacy | 0 | recap |
| `claude-sonnet-3-7` | Sonnet 3.7 | legacy | 1 | rollup / narrative |

At runtime the collector probes every `current` entry via
`claude -p "ping" --model <alias>` (concurrency cap 4, 5s per-model
timeout), caches the result in the `model_availability` DuckDB table
(24h TTL), and exposes the merged view over HTTP:

```sh
curl -s http://localhost:4100/v1/models
```

Response shape:

```json
{
  "models": [
    {
      "alias": "claude-haiku-4-5",
      "short_alias": "haiku",
      "family": "haiku",
      "generation": "4-5",
      "display": "Haiku 4.5",
      "tier": 0,
      "context_k": 200,
      "recommended": ["recap"],
      "status": "current",
      "available": true,
      "checked_at": "2026-04-15T06:30:00Z"
    }
  ],
  "defaults": {
    "recap":     "claude-haiku-4-5",
    "rollup":    "claude-sonnet-4-6",
    "narrative": "claude-sonnet-4-6"
  },
  "refreshed_at": "2026-04-15T06:30:00Z"
}
```

Each worker picks its model via the same resolver chain, in order:

1. **Preference override** — `summarizer.recap_model` (or
   `rollup_model` / `narrative_model`) persisted in `user_preferences`.
2. **Config override** — `[summarizer].recap_model` in `config.toml`.
3. **Catalog resolver** — `ResolveDefaultModel(use_case, availability)`
   walks the catalog in declaration order and picks the first
   `current` entry that is either explicitly available in the cache or
   has no cache entry yet. Legacy entries only win when every `current`
   entry is probed-unavailable.

`summarizer.Default()` no longer ships a hardcoded alias — a fresh
install with no config and no preference automatically gets the
cheapest currently-available `current` entry per tier.

### Phase narrative (tier 3)

The narrative worker chains off the rollup worker and writes a
`phases[]` array onto the same `session_rollups` row. To trigger a
manual refresh:

```sh
curl -s -X POST http://localhost:4100/v1/sessions/<id>/narrative
```

The response is `202 Accepted` with `{"enqueued": true}`. See
[`docs/narrative.md`](narrative.md) for the full tier-3 walkthrough.

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
| `/events/` | **PR #37** — Datadog-style event browser. Left rail `FacetPanel` with collapsible `source_app` / `hook_event` / `severity` / `session` groups showing distinct values + counts, stacked-bar `LogHistogram` above the table (click-drag to zoom into a time range), free-text body search, `?window=` time picker, and cursor pagination. Backed by `GET /v1/events/recent`, `GET /v1/events/facets`, and `GET /v1/events/timeseries`. |
| `GET /v1/events/facets` | **PR #37** — returns the top 50 distinct values + counts per facet dimension (`source_app`, `hook_event`, `severity_text`, `session_id`) matching the supplied filter. Supports `?window=`, `?since=`, `?until=`, `?q=`, `?facets.<key>=a,b` multi-select. Response: `{ "facets": [{ "key": ..., "values": [{ "value": ..., "count": ... }, ...] }, ...] }`. |
| `GET /v1/events/timeseries` | **PR #37** — stacked-bar histogram over the same filter. Uses DuckDB `time_bucket()` to aggregate into `?step=30s` buckets with severity breakdown. Default step scales with the window (1 min → 1 s, 1 h → 30 s, 24 h → 10 min, 7 d → 1 h). Response: `{ "buckets": [{ "bucket": "...", "total": N, "by_severity": {"info": N, "error": N } }, ...], "total": N, "step": "30s" }`. |
| `GET /v1/live/bootstrap` | **PR #37** — consolidated first-paint payload for the Live landing page. Replaces the previous ~7 parallel fetches (`/v1/turns/active`, `/v1/attention/counts`, `/v1/events/recent`, and four `/v1/metrics/series`) with a single round trip returning `{ recent_turns, attention, recent_events, metrics: { active_turns, tools_rate, errors_rate, hitl_pending }, now }`. Cuts initial paint from ~600 ms to ~80 ms on a warm DuckDB. |
| `/settings/` | Collector info and telemetry status. |
| `/styleguide/` | Design tokens and component reference. |
