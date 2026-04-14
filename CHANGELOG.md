# Changelog

All notable changes to apogee will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

This is the initial 11-PR development arc that brings apogee from an empty
scaffold to a single-binary observability dashboard for Claude Code.

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
