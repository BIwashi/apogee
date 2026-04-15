# Changelog

All notable changes to apogee will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **PR #33 — light theme + `data-theme` toggle.** Dashboard gains a second
  palette under `:root[data-theme="light"]`. A TopRibbon toggle cycles
  through `system → light → dark` with localStorage persistence, a
  pre-hydration inline script in `app/layout.tsx` (no flash of wrong
  theme), and automatic tracking of OS-level `prefers-color-scheme`
  changes when the preference is `system`. Settings page gains an
  Appearance section with the same control; styleguide gains a theme
  toggle so designers can verify every token in both themes. The
  `web/app/globals.css` palette is restructured into a shared block, a
  dark block (the default, applied under `:root` and
  `:root[data-theme="dark"]`), a `:root[data-theme="light"]` block, and
  a `prefers-color-scheme: light` fallback for the JS-off case. New
  design-token CSS variables — `--tint-*`, `--shadow-sm`, `--shadow-md`,
  `--shadow-lg`, `--overlay-backdrop`, `--accent-foreground`,
  `--chip-on-accent` — replace every hard-coded hex / rgba literal that
  used to live inside components. `web/app/lib/design-tokens.ts` is
  restructured into `DESIGN_TOKENS.dark`, `DESIGN_TOKENS.light`, and
  `DESIGN_TOKENS.shared` buckets while keeping the legacy top-level
  exports dark-valued for back-compat. No structural changes — every
  component already used CSS variables, so the PR is palette derivation
  + wiring. See [`docs/design-tokens.md`](docs/design-tokens.md) for the
  full palette comparison.

- **PR #32 — phase narrative (summarizer tier 3).** The session rollup
  gains a `phases[]` array: an LLM-generated timeline of semantic phases
  (implement / review / debug / plan / test / commit / delegate / explore /
  other) with per-phase headline, 1–3 sentence narrative, 2–5 key steps,
  kind, duration, turn count, and tool summary. A new narrative worker
  (`internal/summarizer/narrative.go`) runs as tier 3 of the summarizer,
  chained after the tier-2 rollup, with a configurable model (default
  `claude-sonnet-4-6`) and a language / system-prompt preference pair
  (`summarizer.narrative_model`, `summarizer.narrative_system_prompt`).
  New `POST /v1/sessions/:id/narrative` route triggers a manual refresh.
  Session detail gains a new **Timeline** tab (now the default) that
  renders the phases as clickable cards with hover previews and a
  side-drawer full detail (`web/app/components/PhaseTimeline.tsx`,
  `PhaseCard.tsx`, `PhaseDrawer.tsx`). Old rollups without `phases[]`
  still parse. New docs: `docs/narrative.md`, `docs/narrative_ja.md`.

- **PR #31 — `apogee onboard` interactive setup wizard.** New top-level
  `apogee onboard [flags]` subcommand that chains the four install steps
  (hooks + daemon + summarizer preferences + OTLP config) into a single
  walk-through backed by [`charmbracelet/huh`](https://github.com/charmbracelet/huh).
  Every prompt's default is loaded from the live state on disk — existing
  `~/.claude/settings.json` entries, `daemon.Manager.Status(ctx)`, DuckDB
  `user_preferences` rows, and the `[telemetry]` block in
  `~/.apogee/config.toml` — so re-runs are safe and only propose the
  deltas that actually change. Passing `--yes` (or
  `--non-interactive`, or `APOGEE_ONBOARD_NONINTERACTIVE=1`, or piping into a
  non-TTY stdin) drops every prompt for the CI / docker path. Non-interactive
  mode never overwrites a non-empty existing summarizer system prompt with
  an empty default, and never opens the browser. `--dry-run` prints the plan
  box and exits without writing. Skip flags (`--skip-hooks`,
  `--skip-daemon`, `--skip-summarizer`, `--skip-telemetry`) let each step be
  individually disabled. Every apply step is idempotent and delegated to the
  existing package-level helpers (`init.Init`, `daemon.Manager.Install`,
  `duckdb.Store.UpsertPreference`, `openURL`), so the onboard wizard is a
  thin orchestration layer rather than a parallel implementation. First
  failing step aborts with a styled error box; earlier successes are **not**
  rolled back. New files: `internal/cli/onboard.go`,
  `internal/cli/onboard_test.go`, `internal/cli/preferences_adapter.go`,
  `docs/onboard.md`, `docs/ja/onboard.md`. README, README.ja, docs/cli.md,
  docs/ja/cli.md, CLAUDE.md PR arc updated. `github.com/charmbracelet/huh`
  added as a direct dependency (v0.6.0). `github.com/charmbracelet/x/cellbuf`
  and adjacent x/ansi / colorprofile pins were bumped to satisfy the huh
  v0.6.0 → lipgloss v0.13.0 dep graph cleanly alongside `lipgloss/v2`.

### Changed

- **PR #28 — daemon polish + DB lock safety + doctor expansion.** The `apogee daemon {install,uninstall,start,stop,restart,status}` subcommand tree now prints styled lipgloss boxes with semantic color on success / warning / error, and `apogee status` gains the same treatment. The DuckDB `Open` path grows a pre-flight via a new sidecar `<db>.apogee.lock` file — when another apogee process already holds the DB, the collector exits with a styled error box showing the holder's path and PID (best-effort via `lsof`) and a three-step fix instead of the raw DuckDB driver error. `apogee doctor` adds DuckDB lock, collector reachability, and hook install checks (7 checks total), and gains a `--json` flag so CI / scripts / `apogee menubar` can consume them. README, README.ja, docs/cli.md, docs/ja/cli.md, docs/daemon.md, docs/ja/daemon.md, and a new docs/doctor.md all audited and updated so every new affordance is documented with sample output. New file: `internal/store/duckdb/lock.go` — exposes `CheckDBLockHolder`, `AcquireDBLock`, `ErrDBLocked`, `LockedError`. Lipgloss/v2 (`charm.land/lipgloss/v2`) is now a direct dependency.
- **PR #30 — ticker layout fix + paginated events browser.** The live dashboard's event ticker is now pinned directly below the count pills at a fixed max-height of 180 px with internal scroll, so the page no longer shifts every time a new event arrives. A new `/events` route provides a full paginated browser (50 per page, Prev / Next navigation, URL-backed page number) backed by a new `GET /v1/events/recent` endpoint with cursor pagination. Row click opens a Datadog-style side drawer showing the full JSON without a page navigation. Session detail's Logs tab inherits the same height-cap fix.
- **PR #25 — Space Grotesk replaces Artemis Inter.** apogee no longer bundles NASA's Artemis Inter display font (a brand asset of the Artemis program that cannot be redistributed in an open-source project). Display font is now [Space Grotesk](https://github.com/floriankarsten/space-grotesk) by Florian Karsten, SIL Open Font License 1.1, shipped under `web/public/fonts/SpaceGrotesk-{Bold,Medium}.ttf` with a copy of the OFL alongside. Body text continues to use the operating system's native UI stack. Every raster under `assets/branding/` and the Next.js icons have been regenerated with Space Grotesk. A new `docs/credits.md` enumerates the full third-party asset and license list and a Credits section is added to the README.
- **PR #26 — persistent SSE across route navigations.** The Server-Sent Events connection is now hoisted into a single `<SSEProvider>` mounted inside the root `layout.tsx`, so one long-lived `EventSource` is shared by every route instead of being torn down and re-opened on each `router.push()`. `useEventStream` is now a thin selector hook that reads from context and accepts an optional `{ sessionId?, types? }` filter object (the old `path` + options arguments are gone). A ring buffer of 500 events plus an imperative `subscribe()` fan-out let consumers like `InterventionQueue` react to matching events without waiting for a re-render. The top-ribbon LIVE indicator no longer flashes "connecting" when the operator moves between `/`, `/sessions/`, and `/turn/`. Server-side `?session_id=` filtering on `/v1/events/stream` is unchanged for non-browser clients.

### Added

- **PR #29 — summarizer preferences (language + system prompts).** New `user_preferences` DuckDB table (K/V with JSON values) plus `GET /v1/preferences` and `PATCH /v1/preferences` routes. The summarizer now loads `summarizer.language` (`en`/`ja`), `summarizer.recap_system_prompt`, `summarizer.rollup_system_prompt`, and optional model overrides on every job start and injects them into the prompt. Japanese output is fully translated in the instruction block; the TypeScript schema stays English. A new compact language picker on the TopRibbon and a new Summarizer section on the `/settings` page expose the controls to the operator.
- **PR #27 — docs refresh + Japanese translations.** Every file under `docs/` has been audited and brought up to v0.1.3. New files: `docs/cli.md`, `docs/hooks.md`, `docs/data-model.md`, and a full Japanese mirror under `docs/ja/`. `README.ja.md` translates the README into Japanese. The English README gains a language switcher at the top. `CLAUDE.md`'s PR arc list is updated to reflect every PR shipped so far. `CONTRIBUTING.md` audited and fixed, now covers "How to add a new subcommand" and "How to update docs".
- **PR #23 — macOS menu bar app.** New `apogee menubar` subcommand backed by `caseymrm/menuet`. Polls the collector every 5s and renders daemon status, session counts, and quick actions (open dashboard, open logs, restart daemon, quit) in a native macOS status item. Build-tagged so non-darwin builds compile cleanly with a stub that prints a clear "macOS only" message.
- **PR #21 — daemon core (launchd / systemd).** New `apogee daemon {install,uninstall,start,stop,restart,status}` subcommand tree with platform-specific implementations under `internal/daemon/`. macOS uses launchd via `launchctl bootstrap/bootout/kickstart`; Linux uses systemd `--user` units. New top-level convenience commands `apogee status`, `apogee logs`, `apogee open`, and `apogee uninstall`. Extended `~/.apogee/config.toml` with a `[daemon]` block. Foundation for the interactive onboard wizard (PR #22) and the macOS menu bar app (PR #23).
- **PR #20 — Go-native hook, Python removed.** `apogee hook --event X` is the new Claude Code hook invocation. The Python hook library (`hooks/`), the `apogee hooks extract` subcommand, and the embedded hooks filesystem (`hooksfs.go`) are deleted. `apogee init` now writes `<binary path> hook --event X --server-url ...` into `.claude/settings.json` and the `python3` PATH check is removed from `apogee doctor`. Cross-platform: one binary, no Python dependency, no extraction step. Existing installs from v0.1.x still pointing at `python3 send_event.py` are not auto-migrated; run `apogee init --force` to replace them.
- **PR #18 — dynamic source_app + user-scope default for `apogee init`.** The hook derives `source_app` at hook invocation time from `$APOGEE_SOURCE_APP`, then `basename $(git rev-parse --show-toplevel)`, then `basename $PWD`. `apogee init` defaults to user scope and no longer pins `--source-app` into the generated commands, so one install on a machine automatically labels every project with its own repo name. Passing `--source-app foo` still pins a fixed label as before.

## [0.1.0] — 2026-04-15

First tagged release. darwin amd64 + arm64. Linux deferred to v0.2.0 pending a
proper cgo cross-toolchain. This is the initial 11-PR development arc that
brings apogee from an empty scaffold to a single-binary observability
dashboard for Claude Code.

### Added

- **PR #1 — scaffold + design system.** Monorepo layout, Go module, Next.js 16 + Tailwind v4 frontend, Artemis Inter display font, NASA-inspired dark theme, design token spec under `docs/design-tokens.md`.
- **PR #2 — collector core.** DuckDB schema (`sessions`, `turns`, `spans`, `logs`, `metric_points`), stateful trace reconstructor that turns hook events into OTel-shaped spans, `POST /v1/events` ingest API, and read endpoints for sessions/turns/spans/logs.
- **PR #3 — SSE fan-out and live dashboard skeleton.** In-process SSE hub, `/v1/events/stream` endpoint, typed event envelope (`turn.started/updated/ended`, `span.inserted/updated`, `session.updated`), and the live triage table that hydrates from `EventTypeInitial` and reacts to subsequent events without polling.
- **PR #4 — attention engine + KPI strip.** Rule-based attention engine with phase heuristics, history-backed `task_type_history` watchlist bucket, derived `attention_state / reason / score / phase` columns on the `turns` row, and the KPI sparkline strip on the dashboard.
- **PR #5 — turn detail + swim lane + filter chips.** Per-turn detail page with span tree, swim lane, attention reasoning panel, raw log panel, and shareable URL filter chips. Phase segments computed server-side and shipped alongside spans.
- **PR #5.5 — branding assets.** Apogee banner, logo, and icon under `assets/branding/`. README title banner.
- **PR #6 — LLM summarizer.** Per-turn structured recap powered by the local `claude` CLI (Haiku tier). `summarizer.Worker` runs as an async pool, persists the JSON blob onto the `turns` row, and emits `turn.updated` SSE events. `RecapPanels` UI component plus manual regeneration endpoint `POST /v1/turns/:id/recap`.
- **PR #6.5 — global selectors and scoped views.** Datadog-inspired top ribbon with source-app / session / time-range scope, command palette (⌘K), persistent URL state, and scoped KPI / filter-options endpoints.
- **PR #7 — HITL as structured record.** `hitl_events` table, lifecycle service (pending → responded / timeout / expired / error), reason categories, resume modes, response API, and the per-session HITL queue panel on the turn detail page.
- **PR #8 — OTel registry + OTLP integration.** Versioned `claude_code.*` semantic conventions registry (YAML → Go constants), OTel SDK provider, OTLP gRPC and HTTP exporters, mirror of every reconstructor span to the SDK, and the post-hoc recap enrichment span emitted by the summarizer worker.
- **PR #9 — Python hook library + install UX.** Stdlib-only Python hook library under `hooks/`, embedded into the Go binary via `//go:embed all:hooks`. `apogee init` extracts the library to `~/.apogee/hooks/<version>/` and rewrites `.claude/settings.json`. Hook smoke test and Python unittests included.
- **PR #10 — embedded frontend + CLI distribution.** Next.js static export embedded into the Go binary via `embed.FS`, SPA fallback handler in the chi router, cobra-based CLI with `serve / init / doctor / version`, GoReleaser config, and the warning that `go install` produces a placeholder UI.
- **PR #11 — README, screenshots, session rollup polish.** Per-session narrative rollup worker (Sonnet tier) with `session_rollups` table, `RollupWorker`, hourly background scheduler, manual `POST /v1/sessions/:id/rollup` endpoint, and the `RollupPanel` component on the session detail page. Real dashboard screenshots committed under `assets/screenshots/` and rendered in the README. New `CONTRIBUTING.md`, this `CHANGELOG.md`, and a refreshed `README.md`.
- **PR #15 — operator intervention UI.** Composer, queue, and timeline
  components with live SSE updates and staleness indicators. The composer
  is keyboard-first (Ctrl/Cmd+Enter sends, Esc clears, Alt+I focuses),
  sticks mode/scope/urgency between submissions, and colours its left
  border by the selected urgency. The queue marks rows that have been
  queued longer than 30s with a warning pill and upgrades to "no hook
  activity" at 120s — the idle-session safety net surfaced in the UI.
  The turn detail page gains a pulsing header chip that mirrors the
  same staleness tiers, and the session detail page gains a compact
  summary card plus per-turn Intervene buttons that deep-link into the
  composer via `?compose=1`.
- **PR #14 — operator interventions (backend).** Reverse-direction HITL: operators push a message into a live Claude Code session, and the next `PreToolUse` or `UserPromptSubmit` hook returns it as a Claude Code hook decision. Adds the `interventions` DuckDB table, a `queued → claimed → delivered → consumed` lifecycle, atomic claim primitive, `interventions.Service` with an auto-expire sweeper, nine REST endpoints under `/v1/interventions` and `/v1/sessions/{id}/interventions`, six new `intervention.*` SSE broadcast types, attention-engine `intervention_pending` signal, semconv registry `claude_code.intervention.*` group and `claude_code.intervention.delivered` span event, `[interventions]` TOML block with env-var overrides, the `hooks/apogee_intervention.py` helper module plus send-event.py integration, `docs/interventions.md`, and integration tests covering the full lifecycle, the concurrent-claim race, auto-expire, and cancel paths. The matching UI ships in PR #15.

### Changed

- README rewritten around the new screenshots; status table reflects the full 11-PR arc shipped.
- `summarizer.Service` now drives both the per-turn recap worker and the per-session rollup worker, sharing the same `Runner` interface and CLI path.
- Reconstructor exposes `OnSessionEnded` so the rollup worker can produce a final digest the moment a `SessionEnd` hook lands.
- Schema split: rollups live in their own `session_rollups` table to keep the `turns` row narrow.

### Notes

- This release predates any tagged version. APIs, schemas, and the on-disk format remain unstable until v0.1.0.
- Both summarizer tiers shell out to the local `claude` CLI by design — apogee never talks to the Anthropic API directly. If `claude` is not on `PATH` the workers log and skip; the dashboard degrades gracefully.
- Screenshots committed under `assets/screenshots/` are regeneratable end-to-end via `bash scripts/capture-screenshots.sh`.
