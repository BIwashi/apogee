<p align="center">
  <img src="assets/branding/apogee-banner.png" alt="apogee" width="600">
</p>

<p align="center">
  <strong>The highest vantage point over your Claude Code agents.</strong>
</p>

<p align="center">
  <img src="assets/screenshots/dashboard-overview.png" alt="apogee live triage dashboard" width="100%">
  <br>
  <em>Live triage dashboard — sort turns by attention state, scope to a session with ⌘K.</em>
</p>

> The live dashboard above reflects the previous two-column layout. PR #24
> introduces a focus-card driven redesign where the running turn is the
> hero of the landing page; screenshots will be regenerated in the next
> polish pass.

apogee is a single-binary observability dashboard for multi-agent [Claude Code](https://docs.claude.com/en/docs/claude-code) sessions. It captures every hook event, builds OpenTelemetry-shaped traces out of them, stores everything in DuckDB, and streams the result to a dark, NASA-inspired Next.js dashboard that ships embedded in the Go binary.

> [!WARNING]
> apogee is under active development. APIs, schemas, and the on-disk format can break between commits until the first tagged release.

---

## Why apogee

Running multi-agent Claude Code workflows means losing sight of what each agent is actually doing — which tools fire, which permissions get asked, which commands get blocked, which subagent is stuck. apogee answers three questions at a glance:

- **Where should I look right now?** A rule-based attention engine buckets every running turn into `healthy / watchlist / watch / intervene_now` and sorts the live list accordingly, so the noisiest thing is always at the top.
- **What is this turn doing at this exact moment?** Phase heuristics (plan / explore / edit / test / commit / delegate) and a live swim lane render every tool, subagent, and HITL request on a shared time axis.
- **What just happened across the whole session?** A two-tier LLM summarizer fills in a per-turn recap (Haiku) and a per-session narrative rollup (Sonnet), both via the local `claude` CLI — no extra API key required.

---

## Key features

| Surface | What you get |
|---|---|
| Live page | Focus-card driven landing view — the running turn is the hero, with its flame graph, recap headline, phase + current tool, and a CTA to the full turn detail page. A vertical triage rail lists every session with running turns, sorted by attention. |
| Sessions catalog | Searchable, filterable table of every session the collector has seen (Datadog Service Catalog analogue). |
| Agents | Per-agent view with main vs subagent split, invocation counts, rolling duration, parent→child tree. |
| Insights | Aggregate analytics — error rate, duration percentiles, top tools, top phases, watchlist sessions (last 24h). |
| Settings | Collector build metadata + OTel exporter status; config path and daemon/hook install flows surfaced inline. |
| Session detail | Per-session rollup, scoped KPIs, every turn ordered by attention |
| Turn detail | Swim lane, span tree, recap panels, attention reasoning, HITL queue |
| Command palette | Fuzzy search across sessions, scopes, and recent prompts (⌘K) |
| Recap worker | Per-turn structured recap via the local `claude` CLI (Haiku) |
| Rollup worker | Per-session narrative digest via the local `claude` CLI (Sonnet) |
| HITL queue | Permission requests as first-class records with operator decisions |
| Operator interventions | Push text into a live Claude Code session; next `PreToolUse` or `UserPromptSubmit` hook delivers it as `{"decision":"block","reason":...}` or additional context |
| OpenTelemetry | OTLP gRPC/HTTP export, full claude_code.* semconv registry |
| Hooks entry point | `apogee hook --event X` — the binary itself is the hook, zero Python dependency |
| CLI | `serve`, `init`, `hook`, `doctor`, `version` — one binary, no Node or Python runtime |

<p align="center">
  <img src="assets/screenshots/session-detail.png" alt="session detail" width="49%">
  <img src="assets/screenshots/turn-detail.png" alt="turn detail" width="49%">
  <br>
  <em>Session rollup and per-turn swim lane — both populated by the local claude CLI.</em>
</p>

---

## Architecture

```
┌────────────────────────┐      ┌─────────────────────────────────────────────┐
│  Claude Code hooks     │      │  apogee collector  (single Go binary)        │
│  `apogee hook --event` │─POST─│                                              │
│  12 hook events        │ JSON │  ┌─ ingest ────────────────────────────┐    │
└────────────────────────┘      │  │ reconstructor: hook → OTel spans    │    │
                                │  │ per-session agent stack + pending   │    │
                                │  │ tool-use-id map                     │    │
                                │  └────────────────┬────────────────────┘    │
                                │                   │                         │
                                │  ┌─ store/duckdb ─▼────────────────────┐    │
                                │  │ sessions · turns · spans · logs ·   │    │
                                │  │ metric_points · hitl · rollups       │    │
                                │  └────────────────┬────────────────────┘    │
                                │                   │                         │
                                │  ┌─ attention ────▼────────────────────┐    │
                                │  │ rule engine + phase heuristic +      │    │
                                │  │ history-based pre-emptive watchlist  │    │
                                │  └────────────────┬────────────────────┘    │
                                │                   │                         │
                                │  ┌─ summarizer ───▼────────────────────┐    │
                                │  │ recap worker  (Haiku, per turn)      │    │
                                │  │ rollup worker (Sonnet, per session)  │    │
                                │  └────────────────┬────────────────────┘    │
                                │                   │                         │
                                │  ┌─ sse ──────────▼────────────────────┐    │
                                │  │ hub + /v1/events/stream              │    │
                                │  └────────────────┬────────────────────┘    │
                                │                   │                         │
                                │  ┌─ web (Next.js static, embed.FS) ────▼──┐ │
                                │  │ /                 live triage          │ │
                                │  │ /sessions/        session catalog      │ │
                                │  │ /session/?id=     session detail       │ │
                                │  │ /turn/?sess=&turn=  turn detail        │ │
                                │  └────────────────────────────────────────┘ │
                                └─────────────────────────────────────────────┘
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
| Go-native hook subcommand + install UX | shipped |
| Embedded frontend + CLI distribution | shipped |
| README + screenshots + session rollup polish | shipped |

See [open pull requests](https://github.com/BIwashi/apogee/pulls) for what is actively landing next.

---

## Quickstart

```sh
# 1. Install (Homebrew tap, go install, or build from source).
brew install BIwashi/tap/apogee
# or
go install github.com/BIwashi/apogee/cmd/apogee@latest

# 2. Start the collector and install hooks once for every project on this machine.
apogee serve &
apogee init

# 3. Open the dashboard.
open http://localhost:4100
```

That's it. `apogee init` defaults to **user scope** (`~/.claude/settings.json`), so every Claude Code session on this machine reports into the same collector. The `source_app` label is derived dynamically at hook firing time from:

1. `$APOGEE_SOURCE_APP` — explicit override.
2. `basename $(git rev-parse --show-toplevel)` — when the session is inside a git repository.
3. `basename $PWD` — fallback.

So starting `claude` in `~/work/newmo-backend` labels every event `source_app=newmo-backend`, and starting `claude` in `~/work/apogee` labels every event `source_app=apogee`, automatically, with no reconfiguration.

Pin a fixed label with `apogee init --source-app my-project` when you want to override the runtime derivation. Per-project installs are still available via `apogee init --scope project`.

> [!NOTE]
> `go install` produces a binary whose embedded dashboard is a placeholder page: the API is fully functional, but the UI is a stub that instructs you to run `make web-build` locally or install a release binary. This is because the Next.js static export is not distributed through the Go module proxy. `brew install` and the release tarballs always carry the full dashboard.

Once the collector is running, restart Claude Code in any project and every hook event begins streaming into the dashboard.

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
  cli/              cobra commands (serve / init / doctor / version)
  collector/        chi router, server wiring, SSE endpoint
  hitl/             HITL service: lifecycle, expiration, response API
  ingest/           hook payload types, stateful trace reconstructor
  metrics/          background sampler writing to metric_points
  otel/             OTel-shaped Go models
  sse/              fan-out hub + event envelopes
  store/duckdb/     DuckDB schema + queries
  summarizer/       recap worker (Haiku) + rollup worker (Sonnet)
  telemetry/        OTel SDK provider, OTLP exporter
  webassets/        embed.FS for the Next.js static export
  version/          build-version constant
web/                Next.js 16 dashboard (App Router, Tailwind v4)
  app/              routes and React components
  app/lib/          typed API client, SWR hooks, design tokens
  public/fonts/     Artemis Inter display font
assets/branding/    apogee banner, logo, and icon
assets/screenshots/ committed dashboard screenshots
scripts/            screenshot capture (playwright) and fixtures
semconv/            OpenTelemetry semantic conventions for claude_code.*
                    (no hooks/ directory — `apogee hook` is the entry point)
docs/               architecture + design-token + semconv specs
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
apogee status
```

Stop, restart, and tail logs the same way:

```sh
apogee daemon stop
apogee daemon restart
apogee logs -f
apogee open       # opens http://127.0.0.1:4100 in your browser
```

To remove apogee entirely:

```sh
apogee uninstall            # stops daemon, removes hooks, prompts before deleting data
apogee uninstall --purge    # also wipes ~/.apogee
```

The unit file lives at `~/Library/LaunchAgents/dev.biwashi.apogee.plist` on macOS and `~/.config/systemd/user/apogee.service` on Linux. See [`docs/daemon.md`](docs/daemon.md) for the full operator cheatsheet.

To regenerate the screenshots committed under `assets/screenshots/`:

```sh
bash scripts/capture-screenshots.sh
```

The script boots the collector against an in-memory DB, posts a fixture batch, and drives Chromium via playwright.

---

## License

Apache License 2.0. See [LICENSE](LICENSE).
