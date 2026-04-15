# apogee — project guide for Claude Code

apogee is a single-binary observability dashboard for multi-agent Claude Code
sessions. It captures every hook event, stores them in DuckDB, and streams them
to a dark, NASA-inspired Next.js dashboard that ships embedded in the Go binary.

## Repository layout

```
cmd/apogee/         Go entry point (CLI / collector / embedded server)
internal/version/   Build-version string
internal/...        Internal Go packages (attention, cli, collector,
                    daemon, hitl, ingest, interventions, metrics, otel,
                    sse, store/duckdb, summarizer, telemetry, webassets)
web/                Next.js 16 dashboard (App Router, Tailwind v4)
  app/              Routes and React components
  app/lib/          Typed API client, SWR helpers, design tokens
  public/fonts/     Space Grotesk display font (SIL OFL 1.1)
semconv/            OpenTelemetry semantic conventions for claude_code.*
docs/               Architecture, CLI, hooks, data-model, design-tokens,
                    daemon, menubar, interventions, otel-semconv, plus
                    Japanese mirror as docs/*_ja.md siblings
.github/workflows/  CI (go vet/build/test, web typecheck/lint/build)
```

There is NO `hooks/` directory. The Claude Code hook is the `apogee`
binary itself — `.claude/settings.json` points every hook event at
`apogee hook --event <X> --server-url ...`. See
[`docs/hooks.md`](docs/hooks.md) for the full wire contract.

## Common commands

```sh
# Go
go run ./cmd/apogee            # prints "apogee 0.0.0-dev"
go build ./...
go vet ./...
go test ./... -race

# Web (from web/)
npm install
npm run dev                    # Next.js dev server on :3000
npm run typecheck
npm run lint
npm run build

# Orchestrated
make dev                       # collector + web together
make build                     # build Go binary and Next.js bundle
```

## Design system

The visual identity is the product's competitive advantage and is documented in
[`docs/design-tokens.md`](docs/design-tokens.md). Do not introduce alternate
color scales, emoji, or component libraries. lucide-react is the only icon set.
Space Grotesk is the only display font (SIL OFL 1.1); body text uses the
native OS stack.

## Architecture

See [`docs/architecture.md`](docs/architecture.md) for the end-to-end sketch
(hooks → collector → DuckDB → SSE → web UI, with an OTel side channel).

## Building

The collector links against DuckDB through `github.com/marcboeker/go-duckdb/v2`,
which is a cgo binding. A working C toolchain is required:

- macOS: install Xcode Command Line Tools (`xcode-select --install`).
- Linux: install `build-essential` (or the equivalent `gcc` + `libc` headers).

`CGO_ENABLED=1` must be set when running `go build`, `go test`, or `go run`
(this is the default for native builds).

## Hooks subsystem

The Claude Code hook is the `apogee` binary itself: `.claude/settings.json`
points every hook event at `apogee hook --event <X> --server-url ...`,
implemented in [`internal/cli/hook.go`](internal/cli/hook.go). `apogee
init` writes the absolute path of the running binary plus `hook` into
settings.json, so there is no Python runtime dependency, no extraction
step, and no embedded filesystem. Network failures must never break
Claude Code — every error logs to stderr and the subcommand exits 0.
See [`docs/interventions.md`](docs/interventions.md) for the
intervention-delivery contract.

Unit tests live alongside the implementation under
[`internal/cli/hook_test.go`](internal/cli/hook_test.go); run with
`go test ./internal/cli/... -race -count=1`.

## Pull request workflow

- One feature branch per PR, named `feat/<slug>` or `fix/<slug>`.
- Commit messages and PR titles are written in English.
- PR descriptions are written in Japanese (author preference).
- Squash-merge into `main`. CI on the `go` and `web` jobs must be green.
- Never commit `.duckdb` files, `.env*`, or anything under `/data/`.

## Data model

apogee treats **one Claude Code user turn as one OpenTelemetry trace**. The
trace starts at `UserPromptSubmit` and ends at `Stop`. Every tool call,
subagent run, and HITL request inside the turn is a child span. Subagent tool
calls are parented to the subagent span, which is parented to the turn root.

Storage is DuckDB, with OTel-shaped tables for `spans`, `logs`, and
`metric_points`, plus denormalized `sessions`, `turns`, `hitl_events`, and
`session_rollups` tables for fast dashboard rendering. Attention state and
the per-turn recap blob are derived and written back onto the `turns` row.
Per-session narrative digests live in `session_rollups`, written by the
summarizer's second tier (`internal/summarizer/rollup.go`).

## Screenshots

Real dashboard screenshots live under [`assets/screenshots/`](assets/screenshots/)
and are referenced from the README. Regenerate them end-to-end with:

```sh
bash scripts/capture-screenshots.sh
```

The script boots the collector against an in-memory DB, posts a fixture
batch from `scripts/screenshot_fixtures.json`, and drives Chromium via
playwright (installed locally into `scripts/node_modules/`).

## Operator Queue

The turn detail page exposes an **Operator Queue** section (above the
Recap / HITL grid) where an operator can push a message into a live
Claude Code session via the `InterventionComposer`. The composer,
`InterventionQueue`, and `InterventionTimeline` components live under
`web/app/components/` and are composed by `OperatorQueueSection`.
`Alt+I` on the turn detail page focuses the composer; the section
header carries the shortcut as a `kbd` hint. See
[`docs/interventions.md`](docs/interventions.md) for the end-to-end
walkthrough.

## PR arc

Shipped and merged into `main`:

- PR #1 — scaffold + design system (shipped)
- PR #2 — collector core: DuckDB + trace reconstructor + ingest HTTP (shipped)
- PR #3 — SSE fan-out + live dashboard skeleton (shipped)
- PR #4 — attention engine + KPI strip (shipped)
- PR #5 — turn detail + swim lane + filter chips (shipped)
- PR #5.5 — branding assets (banner, logo, icon) (shipped)
- PR #6 — LLM summarizer: per-turn recap via Haiku CLI subprocess (shipped)
- PR #6.5 — global selectors, scoped views, command palette (shipped)
- PR #7 — HITL as structured record (shipped)
- PR #8 — OTel registry + OTLP integration (shipped)
- PR #9 — Python hook library + install UX (shipped, later superseded by PR #20)
- PR #10 — embed frontend + CLI + distribution (shipped)
- PR #11 — polish: README, screenshots, session rollup (shipped)
- PR #14 — operator interventions (backend) (shipped)
- PR #15 — operator intervention UI (shipped)
- PR #18 — dynamic source_app + user-scope default for `apogee init` (shipped)
- PR #19 — fang help styling + README local dev hygiene (shipped)
- PR #20 — Go-native hook, Python library removed (shipped)
- PR #21 — daemon core: launchd / systemd `--user` supervisor (shipped)
- PR #23 — macOS menu bar app (`apogee menubar`) (shipped)
- PR #24 — UI redesign: Live focus, proper information architecture (shipped)

In flight (may or may not be merged by the time you read this):

- PR #22 — `apogee onboard` interactive setup wizard (initial spec; replaced by PR #31)
- PR #25 — Replace the display font with Space Grotesk + credits page
- PR #26 — Persistent SSE via a layout-scoped provider
- PR #27 — Docs refresh + full Japanese translations
- PR #31 — `apogee onboard` interactive setup wizard (huh-backed, idempotent, `--yes`/`--dry-run`)
- PR #32 — phase narrative (summarizer tier 3) + Timeline tab on session detail. Adds `phases[]` to `session_rollups.rollup_json`, chains off the tier-2 rollup worker, exposes `POST /v1/sessions/:id/narrative` for manual refresh. See [`docs/narrative.md`](docs/narrative.md).
- PR #36 — cross-cutting Datadog-style SideDrawer across `/agents`, `/sessions`, the session-detail Turns tab, and the turn-detail span tree. URL-driven state via `?drawer=…`, recursive in-place navigation, plain click → drawer / `Cmd+Click` → full page. Adds the `SessionLabel` component so every table that shows a `session_id` also shows source_app + headline. New backend routes `GET /v1/agents/:id/detail` and `GET /v1/spans/:trace_id/:span_id/detail`. See [`docs/drawer.md`](docs/drawer.md).
- PR #38 — Watchdog anomaly detection + header bell. A background worker (`internal/watchdog/`) reads `metric_points`, computes a rolling 24h baseline, and emits `watchdog_signals` rows when the latest 60s window deviates by more than 3σ. The TopRibbon gains a bell icon with an unread badge that opens a `WatchdogDrawer` listing the anomalies. New routes `GET /v1/watchdog/signals` and `POST /v1/watchdog/signals/:id/ack`; new SSE event `watchdog.signal`. See [`docs/watchdog.md`](docs/watchdog.md).
