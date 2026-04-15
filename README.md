<p align="right">English / <a href="README_ja.md">日本語</a></p>

<p align="center">
  <img src="assets/branding/apogee-banner.png" alt="apogee" width="600">
</p>

<p align="center">
  <strong>The highest vantage point over your Claude Code agents.</strong>
</p>

<p align="center">
  <img src="assets/screenshots/dashboard-overview.png" alt="apogee live triage dashboard" width="100%">
  <br>
  <em>Live focus dashboard — the running turn is the hero, with the triage rail listing every session with running turns sorted by attention.</em>
</p>

apogee is a single-binary observability dashboard for multi-agent [Claude Code](https://docs.claude.com/en/docs/claude-code) sessions. It captures every hook event, builds OpenTelemetry-shaped traces out of them, stores everything in DuckDB, and streams the result to a dark, NASA-inspired Next.js dashboard that ships embedded in the Go binary.

> [!WARNING]
> apogee is under active development. APIs, schemas, and the on-disk format can break between commits until the first tagged release.

---

## Why apogee

Running multi-agent Claude Code workflows means losing sight of what each agent is actually doing — which tools fire, which permissions get asked, which commands get blocked, which subagent is stuck. apogee answers three questions at a glance:

- **Where should I look right now?** A rule-based attention engine buckets every running turn into `healthy / watchlist / watch / intervene_now` and sorts the live list accordingly, so the noisiest thing is always at the top.
- **What is this turn doing at this exact moment?** Phase heuristics (plan / explore / edit / test / commit / delegate) and a live swim lane render every tool, subagent, and HITL request on a shared time axis.
- **What just happened across the whole session?** A three-tier LLM summarizer fills in a per-turn recap (Haiku), a per-session narrative rollup (Sonnet), and a tier-3 **phase narrative** that groups the turns into semantic chunks (implement / review / debug / plan / test / commit / delegate / explore) with a headline, 1–3 sentence narrative, and key steps per phase. Everything goes through the local `claude` CLI — no extra API key required. The default model is no longer hardcoded; apogee picks the cheapest currently-available model per tier from a static catalog that you can override from the Settings page or the `apogee onboard` wizard.

### Customising the summarizer

The summarizer reads its language, an optional operator system prompt, and
optional model overrides from the `user_preferences` DuckDB table on every
job. Two ways to manage them:

- The **Settings** page (`/settings`) has a dedicated **Summarizer** section
  with a language toggle, recap / rollup model override inputs, and two
  system-prompt textareas (2048 char limit each).
- A compact **EN / JA language picker** on the top ribbon flips the recap +
  rollup output language in one click — e.g. `EN ▸ JA` switches every new
  recap to Japanese without touching the schema.

Both controls write to the same `PATCH /v1/preferences` endpoint, so
scripted rollouts work too. See [`docs/cli.md`](docs/cli.md#summarizer-preferences)
for the full HTTP contract and validation rules.

The **phase narrative** (tier 3) ships its own preference keys —
`summarizer.narrative_system_prompt` and `summarizer.narrative_model`
(default `claude-sonnet-4-6`) — and a manual refresh route at
`POST /v1/sessions/:id/narrative`. See [`docs/narrative.md`](docs/narrative.md)
for the schema, chaining, staleness guards, and cost estimate.

---

## Key features

| Surface | What you get |
|---|---|
| Live page | Focus-card driven landing view — the running turn is the hero, with its flame graph, recap headline, phase + current tool, and a CTA to the full turn detail page. A vertical triage rail lists every session with running turns, sorted by attention. |
| Sessions catalog | Searchable, filterable table of every session the collector has seen (Datadog Service Catalog analogue). |
| Agents | Per-agent view with main vs subagent split, invocation counts, rolling duration, parent→child tree. |
| Insights | Aggregate analytics — error rate, duration percentiles, top tools, top phases, watchlist sessions (last 24h). |
| Events browser | `/events/` — paginated table of every stored hook event (50 per page, Prev / Next, URL-backed page number, side-drawer JSON inspector). The Live dashboard's event ticker is now height-capped at 180 px with internal scroll, so new events no longer push the page around. |
| Cross-cutting drawers | Datadog-style row-click pattern across `/agents`, `/sessions`, the session detail Turns tab, and the turn detail span tree. Plain click slides a detail drawer in from the right edge with the entity bundle (no navigation away); `Cmd+Click` still opens the full page in a new tab. State lives in `?drawer=…` so deep links and the back button work. See [`docs/drawer.md`](docs/drawer.md). |
| Settings | Collector build metadata + OTel exporter status; config path and daemon/hook install flows surfaced inline. |
| Session detail | Timeline tab (phase narrative) as the default, plus per-session rollup, scoped KPIs, and every turn ordered by attention |
| Turn detail | Swim lane, span tree, recap panels, attention reasoning, HITL queue |
| Command palette | Fuzzy search across sessions, scopes, and recent prompts (⌘K) |
| Recap worker | Per-turn structured recap via the local `claude` CLI (Haiku) |
| Rollup worker | Per-session narrative digest via the local `claude` CLI (Sonnet) |
| Phase narrative | Tier-3 worker that groups every closed turn into semantic phases with a headline, 1–3 sentence narrative, key steps, kind chip, duration, and tool summary — rendered as a clickable Timeline tab with a side-drawer detail view on `/session` |
| HITL queue | Permission requests as first-class records with operator decisions |
| Operator interventions | Push text into a live Claude Code session; next `PreToolUse` or `UserPromptSubmit` hook delivers it as `{"decision":"block","reason":...}` or additional context |
| OpenTelemetry | OTLP gRPC/HTTP export, full claude_code.* semconv registry |
| Hooks entry point | `apogee hook --event X` — the binary itself is the hook, zero Python dependency |
| Background service | `apogee daemon {install,uninstall,start,stop,restart,status}` — launchd (macOS) / systemd `--user` (Linux), styled lipgloss output |
| macOS menu bar | `apogee menubar` — native status item polling the local collector |
| Doctor | `apogee doctor` — 7 environment checks (home, claude CLI, db path, config, DB lock, collector, hook install) with `--json` for scripts |
| CLI | `serve`, `init`, `onboard`, `hook`, `daemon`, `status`, `logs`, `open`, `uninstall`, `menubar`, `doctor`, `version` — one binary, no Node or Python runtime |
| Interactive setup | `apogee onboard` — one-command wizard chaining hooks + daemon + summarizer + OTel + browser, re-runnable safely, with a `--yes` non-interactive path for CI / Docker provisioning |
| Design | Light and dark themes with automatic `prefers-color-scheme` detection and a toggle in the top ribbon. See [`docs/design-tokens.md`](docs/design-tokens.md) for the full palette spec. |

<p align="center">
  <img src="assets/screenshots/session-detail.png" alt="session detail" width="49%">
  <img src="assets/screenshots/turn-detail.png" alt="turn detail" width="49%">
  <br>
  <em>Session rollup and per-turn swim lane — both populated by the local claude CLI.</em>
</p>

---

## Architecture

```
┌────────────────────────┐      ┌──────────────────────────────────────────────┐
│  Claude Code hooks     │      │  apogee collector  (single Go binary)         │
│  `apogee hook --event` │─POST─│                                               │
│  12 hook events        │ JSON │  ┌─ ingest ──────────────────────────────┐   │
└────────────────────────┘      │  │ reconstructor: hook → OTel spans      │   │
                                │  │ per-session agent stack + pending     │   │
                                │  │ tool-use-id map                       │   │
                                │  └────────────────┬──────────────────────┘   │
                                │                   │                           │
                                │  ┌─ store/duckdb ─▼──────────────────────┐   │
                                │  │ sessions · turns · spans · logs ·      │   │
                                │  │ metric_points · hitl_events ·          │   │
                                │  │ session_rollups · interventions ·      │   │
                                │  │ task_type_history                      │   │
                                │  └────────────────┬──────────────────────┘   │
                                │                   │                           │
                                │  ┌─ attention ────▼──────────────────────┐   │
                                │  │ rule engine + phase heuristic +        │   │
                                │  │ history-based pre-emptive watchlist    │   │
                                │  └────────────────┬──────────────────────┘   │
                                │                   │                           │
                                │  ┌─ summarizer ───▼──────────────────────┐   │
                                │  │ recap worker   (Haiku, per turn)       │   │
                                │  │ rollup worker  (Sonnet, per session)   │   │
                                │  └────────────────┬──────────────────────┘   │
                                │                   │                           │
                                │  ┌─ interventions ▼──────────────────────┐   │
                                │  │ queued → claimed → delivered → consumed│  │
                                │  │ atomic claim primitive + sweeper       │   │
                                │  └────────────────┬──────────────────────┘   │
                                │                   │                           │
                                │  ┌─ sse ──────────▼──────────────────────┐   │
                                │  │ hub + /v1/events/stream                │   │
                                │  └────────────────┬──────────────────────┘   │
                                │                   │                           │
                                │  ┌─ telemetry ────▼──────────────────────┐   │
                                │  │ OTel SDK + OTLP gRPC/HTTP exporter     │   │
                                │  └────────────────┬──────────────────────┘   │
                                │                   │                           │
                                │  ┌─ web (Next.js static, embed.FS) ──────▼─┐ │
                                │  │ /            live focus dashboard       │ │
                                │  │ /sessions/   service catalog            │ │
                                │  │ /session?id= session detail + rollup    │ │
                                │  │ /turn?sess=  turn detail + operator queue│ │
                                │  │ /agents      per-agent main/sub view    │ │
                                │  │ /insights    aggregate analytics        │ │
                                │  │ /events/     paginated event browser    │ │
                                │  │ /settings    collector info + OTel      │ │
                                │  └─────────────────────────────────────────┘ │
                                └──────────────────────────────────────────────┘

                                   ┌────────────┐              ┌─────────────┐
                                   │ daemon     │──launchctl──▶│ launchd     │
                                   │ supervisor │──systemctl──▶│ systemd user│
                                   └────────────┘              └─────────────┘
                                   ┌────────────┐
                                   │ menubar    │ (macOS status item, polls /v1/*)
                                   └────────────┘
```

### Data model

apogee treats **one Claude Code user turn as one OpenTelemetry trace**:

```
trace = claude_code.turn  (root span, opens at UserPromptSubmit, closes at Stop)
├── span  claude_code.tool.Bash
├── span  claude_code.tool.Read
├── span  claude_code.subagent.Explore      (subagent child)
│   ├── span  claude_code.tool.Grep
│   └── span  claude_code.tool.Read
├── span  claude_code.hitl.permission       (stays open until a human responds)
└── span event  claude_code.notification
```

Backing storage is DuckDB with OTel-shaped tables for `spans`, `logs`, `metric_points`, plus denormalized `sessions`, `turns`, `hitl_events`, and `session_rollups` for fast dashboard reads. The `turns` row also holds the derived `attention_state`, `attention_reason`, `phase`, and `recap_json` columns. See [`docs/architecture.md`](docs/architecture.md) and [`internal/store/duckdb/schema.sql`](internal/store/duckdb/schema.sql).

---

## Status

| Area | State |
|---|---|
| Monorepo scaffold + design system | shipped |
| Collector core: DuckDB + trace reconstructor | shipped |
| SSE fan-out + live dashboard skeleton | shipped |
| Attention engine + KPI strip | shipped |
| Turn detail + swim lane + filter chips | shipped |
| LLM summarizer (Haiku per turn, Sonnet per session) | shipped |
| HITL as structured record | shipped |
| OpenTelemetry semconv registry + OTLP export | shipped |
| Embedded frontend + CLI distribution | shipped |
| README + screenshots + session rollup polish | shipped |
| Operator interventions (backend + UI) | shipped |
| Go-native hook, Python library removed | shipped |
| Daemon (launchd / systemd `--user`) | shipped |
| macOS menu bar app | shipped |
| UI redesign — Live focus, proper information architecture | shipped |

See [open pull requests](https://github.com/BIwashi/apogee/pulls) for what is actively landing next.

---

## Quickstart

The happy path is two commands. The rest of this section is what each command actually does, what you should see when it works, and how to recover when it does not.

### 1. Install the binary

```sh
brew install BIwashi/tap/apogee
```

Alternative paths:

| Source | Command | Notes |
|---|---|---|
| Homebrew tap | `brew install BIwashi/tap/apogee` | Recommended. Universal binary, full embedded dashboard, `brew upgrade apogee` for new releases. |
| `go install` | `go install github.com/BIwashi/apogee/cmd/apogee@latest` | Go module proxy cannot ship the Next.js bundle, so the UI is a placeholder that tells you to run `make web-build`. The API is fully functional. Use this only when you know you want a CLI-only binary. |
| Release tarball | Download from [Releases](https://github.com/BIwashi/apogee/releases) | darwin amd64 / arm64. Linux is deferred to v0.2.0. |
| Build from source | `git clone ... && make build` | Produces `./bin/apogee` stamped with the commit and build date. |

Verify:

```sh
$ apogee version
apogee v0.1.6 (commit 7345124, built 2026-04-15T05:39:33Z, go1.25.0)
```

### 2. Run the onboarding wizard

```sh
apogee onboard
```

`apogee onboard` is a single interactive command that walks you through every piece of the install in one shell prompt. It is designed to be **re-runnable**: every prompt's default is loaded from the current state on disk (`~/.apogee/config.toml` + DuckDB preferences + `~/.claude/settings.json` + live daemon status), so running it again after a version bump or a tweak is always safe.

The wizard covers five things:

1. **Claude Code hooks** — writes one entry per hook event into `~/.claude/settings.json` pointing at the `apogee hook --event X` subcommand of your current binary. User scope by default, so every project on the machine reports into the same collector. The `source_app` label is derived automatically at hook firing time from `$APOGEE_SOURCE_APP` → `git rev-parse --show-toplevel` → `$PWD`. No per-project configuration.
2. **Background service (daemon)** — registers apogee as a `launchd` user agent (macOS) at `~/Library/LaunchAgents/dev.biwashi.apogee.plist` or a `systemd --user` unit (Linux) at `~/.config/systemd/user/apogee.service`. Logs go to `~/.apogee/logs/apogee.{out,err}.log`. The daemon auto-starts on login and auto-restarts on crash.
3. **LLM summarizer** — asks for the output language (`en` / `ja`) and optional system prompts for the per-turn recap (Haiku), per-session rollup (Sonnet), and phase narrative (Sonnet). Everything persists into DuckDB under `user_preferences` and can be changed later from the Settings page in the dashboard.
4. **OpenTelemetry export** (optional) — if you want traces forwarded to an external collector (Tempo, Honeycomb, Datadog, etc.), wire the OTLP endpoint and protocol here.
5. **Open the dashboard** — starts the daemon and opens `http://127.0.0.1:4100/` in your default browser.

Sample run (fresh machine, accepting every default):

```
APOGEE ONBOARD

  This will:
    1. Install hooks into ~/.claude/settings.json
    2. Install apogee as a user-scope background service
    3. Configure the LLM summarizer
    4. Optionally wire an OTLP endpoint
    5. Start the daemon and open the dashboard

? Install Claude Code hooks at user scope?          › Yes
? Install apogee as a background service?           › Yes
? Start the daemon immediately after install?       › Yes
? Summarizer output language                        › en
? Recap system prompt (optional, leave empty)       › (empty)
? Rollup system prompt (optional, leave empty)      › (empty)
? Narrative system prompt (optional, leave empty)   › (empty)
? Forward traces to an external OTLP endpoint?      › No
? Open the dashboard after everything is wired?     › Yes

? Apply these changes?                              › Yes

Applying...
  ✓ wrote ~/.apogee/config.toml
  ✓ installed 12 hook events into ~/.claude/settings.json
  ✓ installed launchd unit dev.biwashi.apogee
  ✓ wrote summarizer preferences (language=en)
  ✓ daemon started (pid 62341)
  ✓ opened http://127.0.0.1:4100/ in your browser

apogee is ready.
  Run `apogee status` to check the daemon.
  Run `apogee doctor` to verify the environment.
```

Flags for the non-interactive / scripting path:

| Flag | Effect |
|---|---|
| `--yes` / `--non-interactive` | Skip every prompt, accept current-state defaults. Provisioning / CI. |
| `--dry-run` | Print the plan and exit without writing anything. |
| `--skip-hooks` | Do not touch `~/.claude/settings.json`. |
| `--skip-daemon` | Do not install / start the daemon. |
| `--skip-summarizer` | Do not write summarizer preferences. |
| `--skip-telemetry` | Do not touch the `[telemetry]` block. |

The full walkthrough and per-flag reference live in [`docs/onboard.md`](docs/onboard.md).

### 3. Start a Claude Code session

The daemon is now listening on `http://127.0.0.1:4100` and every Claude Code session on this machine reports into it. Start a session in any project:

```sh
cd ~/work/my-project
claude
```

Within a second or two the dashboard lights up:

- **Live page** (`/`): a **Triage Rail** on the left lists every running turn sorted by attention state; a **Focus Card** on the right renders the selected turn's flame graph, current phase, current tool, and a headline from the per-turn recap the moment it lands. The event ticker above runs at a fixed height and does not push the page as new events arrive.
- **Top ribbon**: the **LIVE** dot stays green because the SSE connection is hoisted into the app-level provider and survives every route navigation. Four selectors live here — source_app, session, time range, language (EN / JA) — plus a theme toggle (System / Light / Dark).
- **Session detail** (`/session/?id=<id>`): opens the **Timeline** tab by default. The timeline renders LLM-generated phases (implement / review / debug / plan / test / commit / delegate / explore / other) as clickable cards. Hover a card for a 350 ms preview; click to open a side drawer with the full narrative, key steps, tool summary bar chart, and the list of turns inside the phase.
- **Turn detail** (`/turn/?sess=<sess>&turn=<turn>`): swim lane + span tree + the three-panel recap + HITL queue + the operator intervention composer.
- **Sessions / Agents / Insights / Events / Settings** pages in the sidebar.

### 4. Verify the environment

Any time the setup feels off, run:

```sh
$ apogee doctor

apogee doctor

  ✓ /Users/shota/.apogee writable
  ✓ claude CLI on PATH (/Users/shota/.local/bin/claude)
  ✓ default db path /Users/shota/.apogee/apogee.duckdb
  ✓ no config file (defaults in use)
  ✓ DuckDB file is unlocked
  ✓ collector ok (http://127.0.0.1:4100/v1/healthz, 1 ms)
  ✓ apogee hook installed for 12/12 events

7 ok · 0 warnings · 0 errors
```

Pass `--json` when you want to feed the checks into a script or into `apogee menubar`.

### 5. Daily operation

```sh
apogee status          # daemon + collector + recent activity at a glance
apogee logs -f         # tail the daemon stdout + stderr
apogee open            # open http://127.0.0.1:4100/ in the default browser
apogee daemon restart  # after a version bump or a config tweak

apogee daemon stop     # stop the service (does not uninstall)
apogee uninstall       # remove daemon + hooks + optionally wipe ~/.apogee
```

On macOS, start the menu bar companion app for a live status icon with quick actions:

```sh
apogee menubar &
```

### Troubleshooting

**`DuckDB file is locked` on start-up.**

Another apogee process is holding the DB. Usually this is a stale `apogee serve` from before the daemon install. Kill it and restart:

```sh
pkill -f "apogee serve"
lsof /Users/shota/.apogee/apogee.duckdb   # should print nothing
apogee daemon restart
```

On the second start apogee writes `<db>.apogee.lock` + `<db>.apogee.pid` sidecars and prints a styled error box with the holder's PID, path, and a three-step fix — the raw DuckDB driver error never reaches the operator.

**`LIVE` indicator is red.**

The collector is unreachable. Run `apogee daemon status`. If the daemon is not running, `apogee daemon start`. If the daemon is running but the collector probe fails, `apogee logs -f` and check `apogee.err.log` for the cause.

**Hooks are installed but nothing reaches the dashboard.**

Run `apogee doctor` — check that the `apogee hook installed for 12/12 events` row is green. If Claude Code was running before you ran `apogee init` / `apogee onboard`, restart the Claude Code session so it picks up the new `~/.claude/settings.json`.

**`go install` users see a placeholder dashboard.**

That is expected. The Next.js static export cannot travel through the Go module proxy. Either run `make web-build` from a local checkout, or install via `brew` / the release tarballs which always carry the full dashboard.

**The daemon keeps restarting.**

`apogee logs -f` will show the exit reason. The most common cause is a DuckDB lock conflict (see above). The second most common is a stale plist pointing at a deleted binary after a `brew uninstall` — run `apogee daemon uninstall && apogee daemon install` to rewrite it.

---

## Configuration

apogee reads an optional TOML file at `~/.apogee/config.toml`. Every value has a default so the file is purely additive.

```toml
[telemetry]
enabled       = true
endpoint      = "https://otlp.example.com"
protocol      = "grpc"           # "grpc" or "http"
service_name  = "apogee"
sample_ratio  = 1.0

[summarizer]
enabled       = true
recap_model   = "claude-haiku-4-5"
rollup_model  = "claude-sonnet-4-6"
concurrency   = 1
timeout_seconds = 120

[daemon]
label         = "dev.biwashi.apogee"
addr          = "127.0.0.1:4100"
db_path       = "~/.apogee/apogee.duckdb"
log_dir       = "~/.apogee/logs"
keep_alive    = true
run_at_load   = true
```

Every value is also overridable via environment variables (e.g. `APOGEE_RECAP_MODEL`, `APOGEE_ROLLUP_MODEL`, `OTEL_EXPORTER_OTLP_ENDPOINT`). See `internal/summarizer/config.go` and `internal/telemetry/config.go` for the full list.

---

## OpenTelemetry integration

Every reconstructor write is mirrored onto a real OTel span via the SDK, so apogee doubles as an OTLP source for any backend (Tempo, Honeycomb, Datadog, etc.). The `claude_code.*` attributes follow a versioned semconv registry shipped in [`semconv/`](semconv/) and documented in [`docs/otel-semconv.md`](docs/otel-semconv.md). Set `OTEL_EXPORTER_OTLP_ENDPOINT` (or the TOML equivalent) and the collector exports automatically.

---

## Repository layout

```
cmd/apogee/         Go entry point (CLI + embedded server)
internal/
  attention/        rule engine, phase heuristic, history reader
  cli/              cobra commands (serve / init / hook / daemon /
                    status / logs / open / uninstall / menubar /
                    doctor / version)
  collector/        chi router, server wiring, SSE endpoint
  daemon/           launchd / systemd --user supervisor
  hitl/             HITL service: lifecycle, expiration, response API
  ingest/           hook payload types, stateful trace reconstructor
  interventions/    operator interventions service (queued → consumed)
  metrics/          background sampler writing to metric_points
  otel/             OTel-shaped Go models
  sse/              fan-out hub + event envelopes
  store/duckdb/     DuckDB schema + queries + migrations
  summarizer/       recap worker (Haiku) + rollup worker (Sonnet)
  telemetry/        OTel SDK provider, OTLP exporter
  webassets/        embed.FS for the Next.js static export
  version/          build-version constant
web/                Next.js 16 dashboard (App Router, Tailwind v4)
  app/              routes and React components
  app/lib/          typed API client, SWR hooks, design tokens
  public/fonts/     Space Grotesk display font (SIL OFL 1.1)
assets/branding/    apogee banner, logo, and icon
assets/screenshots/ committed dashboard screenshots
scripts/            screenshot capture (playwright) and fixtures
semconv/            OpenTelemetry semantic conventions for claude_code.*
                    (`apogee hook` is the hook entry point — no hooks/
                    directory, no Python dependency)
docs/               architecture, CLI, hooks, data-model, design-tokens,
                    daemon, menubar, interventions, otel-semconv, and
                    Japanese mirror as docs/*_ja.md siblings
.github/workflows/  CI (Go vet/build/test, web typecheck/lint/build)
```

---

## Local development

Requirements: Go 1.24+, Node 20+, and a C toolchain (DuckDB is accessed through `github.com/marcboeker/go-duckdb/v2`, which is cgo).

```sh
# Go
go build ./...
go vet ./...
go test ./... -race -count=1

# Web (from web/)
npm install
npm run dev       # Next.js dev server on http://localhost:3000
npm run typecheck
npm run lint
npm run build

# Run the collector (from repo root)
go run ./cmd/apogee serve --addr :4100 --db .local/apogee.duckdb
```

The collector by itself is just a server — the dashboard will stay empty until a Claude Code session is wired to report events into it. After the collector is up, install the hooks once at user scope using the **local** binary (not the brew-installed one) so every Claude Code session on this machine streams into your dev collector:

```sh
# After the collector is running, install hooks once at user scope.
make build                    # produces ./bin/apogee
./bin/apogee init             # writes ~/.claude/settings.json
```

After that, `claude` started in any project reports into the local collector and the dashboard lights up.

Or use the Makefile:

```sh
make build            # builds ./bin/apogee
make run-collector    # runs the collector against .local/apogee.duckdb
make test             # go vet + race tests
make dev              # collector and Next.js dev server together
```

`make dev` already starts both the collector and the Next.js dev server, so `make dev` + `./bin/apogee init` is the minimal setup for a new contributor.

> If `make dev` fails with *"address already in use"* on `:4100`, an old collector is still bound to the port. Find it with `lsof -nP -iTCP:4100 -sTCP:LISTEN` and stop it with `pkill -f "apogee serve"`.

---

## Run apogee as a background service

Once you have apogee installed, register it as a launchd (macOS) or systemd user (Linux) service so it starts on every login:

```sh
apogee daemon install
apogee daemon start
apogee daemon status
```

`apogee daemon install` prints a styled success box (NO_COLOR=1 sample shown — colors are bold by default in a TTY):

```
╭───────────────────────────────────────────────────────────────────────╮
│ ✓ daemon installed                                                    │
│                                                                       │
│ Label:      dev.biwashi.apogee                                        │
│ Unit path:  /Users/me/Library/LaunchAgents/dev.biwashi.apogee.plist   │
│ Collector:  http://127.0.0.1:4100                                     │
│ Logs:       /Users/me/.apogee/logs/apogee.{out,err}.log               │
│                                                                       │
│ The daemon will start automatically on next login. To start it now:   │
│   apogee daemon start                                                 │
╰───────────────────────────────────────────────────────────────────────╯
```

`apogee daemon status` renders a Daemon box (info border) and a Collector box (success border when reachable, error border when unreachable):

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

Stop, restart, and tail logs the same way:

```sh
apogee daemon stop      # ✓ daemon stopped (dev.biwashi.apogee)
apogee daemon restart   # ✓ daemon restarted (dev.biwashi.apogee)
apogee logs -f          # tail ~/.apogee/logs/apogee.{out,err}.log
apogee open             # opens http://127.0.0.1:4100 in your browser
```

`apogee logs -f` tails both `apogee.out.log` and `apogee.err.log` from `~/.apogee/logs/`, seeded with the last 50 lines:

```
==> /Users/me/.apogee/logs/apogee.out.log <==
{"time":"2026-04-15T13:01:38+09:00","level":"INFO","msg":"collector listening","addr":"127.0.0.1:4100"}
{"time":"2026-04-15T13:01:38+09:00","level":"INFO","msg":"summarizer: starting","recap_model":"claude-haiku-4-5"}
```

`apogee open` is a thin wrapper over `open` (macOS) / `xdg-open` (Linux) that prints the URL when the system helper is unavailable:

```
Opening http://127.0.0.1:4100/
```

To remove apogee entirely:

```sh
apogee uninstall            # stops daemon, removes hooks, prompts before deleting data
apogee uninstall --purge    # also wipes ~/.apogee
```

`apogee daemon uninstall` (used by `apogee uninstall` internally) renders an info box:

```
╭─────────────────────────────╮
│ daemon uninstalled          │
│                             │
│ Label:  dev.biwashi.apogee  │
╰─────────────────────────────╯
```

The unit file lives at `~/Library/LaunchAgents/dev.biwashi.apogee.plist` on macOS and `~/.config/systemd/user/apogee.service` on Linux. See [`docs/daemon.md`](docs/daemon.md) for the full operator cheatsheet and [`docs/doctor.md`](docs/doctor.md) for the doctor checks reference.

To regenerate the screenshots committed under `assets/screenshots/`:

```sh
bash scripts/capture-screenshots.sh
```

The script boots the collector against an in-memory DB, posts a fixture batch, and drives Chromium via playwright.

---

## Troubleshooting

### DuckDB lock conflict

Apogee writes a sidecar lock file (`<db>.apogee.lock`) and a sidecar pid file (`<db>.apogee.pid`) next to its DuckDB store. If you accidentally start a second collector pointed at the same DB, the second invocation exits 1 with a styled error box instead of the raw driver error:

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

Run `apogee daemon stop` (or `kill <pid>` for an unmanaged collector), then re-run the command. The holder PID is detected via `lsof -nP <db>` when available, with a fallback to the pid file.

### Daemon won't start

- `apogee daemon status` prints the install + load state and the collector reachability box.
- `apogee logs -f` tails `~/.apogee/logs/apogee.{out,err}.log` from the daemon's stdout/stderr.
- On launchd: check `launchctl print gui/$(id -u)/dev.biwashi.apogee` for the supervisor's view.
- On systemd: `journalctl --user -u apogee.service -f` for the unit's logs.

### Hook not firing

Run `apogee doctor` — the `hook_install` check reads `~/.claude/settings.json` and verifies every event in `internal/cli/init.go::HookEvents` points at the apogee binary:

```
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

`apogee doctor --json` emits the same checks as a JSON array suitable for CI / scripts / `apogee menubar`.

If `hook_install` reports `partial` or `missing`, run `apogee init --force` to rewrite the entries.

---

## Credits

- **Display font**: [Space Grotesk](https://github.com/floriankarsten/space-grotesk) by Florian Karsten, [SIL Open Font License 1.1](https://scripts.sil.org/OFL).
- **Body font**: system stack (San Francisco / Segoe UI / Helvetica Neue).
- **Icons**: [lucide](https://lucide.dev) (ISC).
- **Go libraries**: see [`docs/credits.md`](docs/credits.md) for the full list.
- **Inspirations**: [aperion](https://github.com/BIwashi/aperion), [mitou-adv](https://github.com/MichinokuAI/mitou-adv), [disler's observability prototype](https://github.com/disler/claude-code-hooks-multi-agent-observability), and Datadog APM's control plane.

apogee does not bundle any NASA brand asset. The color palette is inspired by NASA Artemis-program hues but uses generic hex values and makes no affiliation claim.

---

## License

Apache License 2.0. See [LICENSE](LICENSE).
