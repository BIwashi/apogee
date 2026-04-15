# apogee onboard — interactive setup wizard

`apogee onboard` is the one-command setup flow for a fresh machine.
It chains the four install steps you would otherwise run by hand:

1. Install Claude Code hooks into `~/.claude/settings.json`.
2. Install apogee as a user-scope background service
   (launchd on macOS, systemd `--user` on Linux).
3. Configure the LLM summarizer — language, optional system
   prompts, and the three model overrides (recap / rollup /
   narrative) — by writing the canonical `user_preferences` rows into
   DuckDB. PR #35 replaces the free-text model inputs with proper
   dropdowns sourced from the static catalog
   (`internal/summarizer/models.go::KnownModels`); the first option on
   each row is "Use default (Haiku 4.5)" and picks the cheapest
   currently-available entry per tier. Probed-unavailable models are
   filtered out of the list so you can't accidentally pin one. See
   [`docs/cli.md`](cli.md) § *Summarizer model catalog* for the full
   resolver walk.
4. Optionally wire an external OTLP collector by updating the
   `[telemetry]` block in `~/.apogee/config.toml`.

After the apply step it starts the daemon and opens the dashboard in
your default browser. Every prompt's default is loaded from the live
state on disk (`config.toml`, DuckDB preferences, `settings.json`,
daemon manager status), so re-runs are safe and only propose the
deltas you actually want to change.

## Quickstart

```sh
# Fully interactive — walk through the wizard.
apogee onboard

# Accept every default, no prompts, no browser. This is the
# provisioning / CI path.
apogee onboard --yes

# Preview the plan without writing anything.
apogee onboard --dry-run

# Only do one step — skip the rest.
apogee onboard --skip-daemon --skip-telemetry
```

## Flags

| Flag | Default | Description |
| --- | --- | --- |
| `--yes`, `-y` | `false` | Accept every default without prompting |
| `--non-interactive` | `false` | Alias for `--yes`, clearer in scripts |
| `--config` | `~/.apogee/config.toml` | Config file to write |
| `--db` | `~/.apogee/apogee.duckdb` | DuckDB file for preferences |
| `--addr` | `127.0.0.1:4100` | Collector bind address |
| `--skip-daemon` | `false` | Do not install / start the daemon |
| `--skip-hooks` | `false` | Do not install hooks into `settings.json` |
| `--skip-summarizer` | `false` | Do not write summarizer preferences |
| `--skip-telemetry` | `false` | Do not configure OTLP export |
| `--dry-run` | `false` | Print the plan and exit |

The environment variable `APOGEE_ONBOARD_NONINTERACTIVE=1` is equivalent
to `--yes`, useful in Docker `RUN` steps and CI smoke tests.

## The plan

Every run builds a plan box before applying. In interactive mode you
get to tab through each section first; in `--yes` mode the plan is
derived straight from the current state.

```
╭───────────────────────────────────────────────────────────────────────────────╮
│ apogee onboard — plan                                                         │
│                                                                               │
│ Config:      /Users/you/.apogee/config.toml                                   │
│ DB:          /Users/you/.apogee/apogee.duckdb                                 │
│ Hooks:       install /Users/you/.claude/settings.json (dynamic source_app)    │
│ Daemon:      install dev.biwashi.apogee @ 127.0.0.1:4100 · start              │
│ Summarizer:  language=en                                                      │
│ OTel:        disabled                                                         │
│ Open:        open http://127.0.0.1:4100/                                      │
╰───────────────────────────────────────────────────────────────────────────────╯
```

Each field maps to one apply step:

| Plan field | Source of truth | Apply step |
| --- | --- | --- |
| `Config` | `--config` (default `~/.apogee/config.toml`) | TOML rewrite |
| `DB` | `--db` (default `~/.apogee/apogee.duckdb`) | DuckDB open |
| `Hooks` | `~/.claude/settings.json` presence | `init.Init(cfg)` |
| `Daemon` | `daemon.Manager.Status(ctx)` | `Manager.Install` + `Start` |
| `Summarizer` | `summarizer.*` preference rows | `Store.UpsertPreference` |
| `OTel` | `[telemetry]` block in `config.toml` | TOML rewrite |
| `Open` | Interactive confirm (always off in `--yes`) | `apogee open` helper |

## Apply output

```
Applying...

✓ wrote ~/.apogee/config.toml
✓ installed 12 hook events into ~/.claude/settings.json
✓ installed dev.biwashi.apogee unit at ~/Library/LaunchAgents/dev.biwashi.apogee.plist
✓ wrote summarizer preferences (language=en)
✓ daemon started (dev.biwashi.apogee)

apogee is ready.
  Run `apogee status` to check the daemon.
  Run `apogee doctor` to verify the environment.
```

Every step prints its own status line via the styled glyph pack
(`✓` / `⚠` / `✗`). The first failing step aborts with a styled error
box, but earlier successes are **not** rolled back — a partial install
is better than undoing completed work on the user's machine.

## Non-interactive path (CI / docker)

When any of the following is true, the wizard treats the run as
non-interactive and drives straight to apply:

- `--yes` or `--non-interactive` was passed.
- `APOGEE_ONBOARD_NONINTERACTIVE=1` is set in the environment.
- `stdin` is not a TTY (for example, when piping into `docker run`).

In non-interactive mode:

- Every prompt's default is applied verbatim.
- Missing state (fresh machine) results in the install path for every
  step.
- Existing, **non-empty** summarizer system prompts are never
  overwritten with an empty default.
- The browser is never opened.

## Idempotence and re-runs

`apogee onboard` is safe to re-run. Each of the four apply steps is
driven by the same package-level helpers used by `apogee init` and
`apogee daemon install`, which are already idempotent. A re-run on an
unchanged machine reports:

```
✓ wrote ~/.apogee/config.toml
✓ installed 0 hook events into ~/.claude/settings.json
✓ installed dev.biwashi.apogee unit at ~/Library/LaunchAgents/dev.biwashi.apogee.plist
✓ wrote summarizer preferences (language=en)
```

Pass `reinstall` in the interactive daemon prompt (or re-run in `--yes`
mode with an already-installed daemon) to force the unit file to be
rewritten in place.

## Failure handling

Every step surfaces its error with a glyph and a short message:

```
✗ daemon install: signalling launchd bootstrap: ...
```

The exit code is `1` on any failing step. `apogee doctor` is the
first tool to reach for when the wizard reports trouble — it covers
the same seven health signals end-to-end.
