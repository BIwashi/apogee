# Changelog

All notable changes to apogee will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [v0.1.12](https://github.com/BIwashi/apogee/compare/v0.1.11...v0.1.12) - 2026-04-15
- fix(upgrade): parse version from rich version.Full() output by @BIwashi in https://github.com/BIwashi/apogee/pull/56

## [v0.1.11](https://github.com/BIwashi/apogee/compare/v0.1.10...v0.1.11) - 2026-04-15
- feat(daemon): auto-restart daemon after brew upgrade by @BIwashi in https://github.com/BIwashi/apogee/pull/54

## [v0.1.10](https://github.com/BIwashi/apogee/compare/v0.1.9...v0.1.10) - 2026-04-15
- ci(release): automate version bumps via Songmu/tagpr by @BIwashi in https://github.com/BIwashi/apogee/pull/49

## [Unreleased]

## [0.1.9] - 2026-04-15

### Added

- **Mission Map forecast tier (PR #51).** The tier-3 narrative LLM
  call now emits a short `forecast[]` array of 0-3 predicted next
  phases alongside the historical `phases[]`. The Mission Map
  extrapolates a dashed approach vector beyond the trailing real
  planet and renders each forecast as a dimmed dashed planet with
  a "NEXT · kind" label, giving the user the "future" half of the
  planetary view — the "未来こうなる予定で、今ここで" piece of the
  original spec. The prompt is explicit that the model should
  return an empty forecast rather than speculate. Reuses the same
  Sonnet call the narrative worker already makes, so the feature
  is effectively free beyond a slightly longer response.
- **Hoverable moons with per-turn recap tooltip (PR #52).**
  Completes the three Mission Map zoom levels: Sun (Sonnet tier-2
  rollup, session goal), planets (Sonnet tier-3 phases), moons
  (Haiku tier-1 per-turn recaps). Each moon now carries a native
  SVG `<title>` that shows the turn's Haiku recap headline on
  hover, an invisible 8 px hit target so hovering is easy, and a
  click handler that navigates to `/turn?id=…`. No floating-tooltip
  library, no extra fetch — the session page already has the
  turns array in memory.

## [0.1.8] - 2026-04-15

### Added

- **Mission Map tab (PR #50).** A new planetary view of the session arc
  added as the default tab on session detail. Reuses the existing
  tier-2 rollup + tier-3 phase narrative to render a solar system:
  Sun = top-level goal, planets = semantic phases along a cubic-bezier
  orbit, moons = individual turns, probe = the running turn (pulsing),
  meteors = operator interventions branching off the phase they were
  injected into. Phase kinds map to lucide icons and the NASA Artemis
  palette so each planet reads as "this kind of work" at a glance.
  The card ships with a CSS-only deep-space starfield background
  (layered radial gradients + nine pixel stars) so no raster assets
  ship. Empty state offers a Generate narrative button that triggers
  the existing tier-3 worker. Clicking a planet opens the existing
  PhaseDrawer side panel. No new backend endpoint, no new LLM tier —
  the visualisation surfaces information the summariser already
  produces.
- **Brew upgrade detection + one-click restart (PR #45).** The
  collector now records the running binary's size + mtime at startup
  and re-stats it every 60 s. When the file changes (typical trigger:
  `brew upgrade apogee`) it shells out to `<path> version` to read
  the new version string and stores it. `/v1/info` gains
  `update_available` / `available_version` /
  `available_version_detected_at`. A new `UpgradeBanner` React
  component polls `/v1/info` every 30 s and renders an accent strip
  above the TopRibbon when an update is available; clicking "Restart
  now" POSTs to the new `POST /v1/daemon/restart` endpoint which
  hands off to `daemon.NewManagerWithLabel(DefaultLabel).Restart(ctx)`
  after a brief flush window. launchctl kickstart -k / systemctl
  --user restart does the actual relaunch. Same-version rebuilds
  (dev flow) refresh the baseline silently so the banner never
  shows for noise.
- **Desktop Homebrew Cask (PR 6a5…).** A second Homebrew tap formula
  ships the Wails macOS desktop shell as `apogee-desktop.app`,
  installable via `brew install --cask BIwashi/tap/apogee-desktop`.
- **Sidebar hover hint popovers (PR #41).** Each sidebar nav item
  gains a short description (e.g. "Live focus dashboard — active
  turns + triage rail") rendered as a CSS-only floating tooltip
  with role tooltip and aria-label. Keyboard focus opens the same
  popover for a11y. Helps first-time users learn what each page
  shows without clicking through.
- **Mission Map brand visuals (PRs #42, #43, #44, #48).** Artemis
  Inter is reinstated as a brand accent alongside Space Grotesk
  (the everyday display face) — the previous PR #25 wholesale swap
  made every ALL-CAPS label unreadable at 10–14 px. Added
  `--font-display-accent` CSS variable and a `.font-display-accent`
  utility class used by the APOGEE wordmark (sidebar, TopRibbon)
  and a handful of hero h1s (Live, Events, Styleguide). The README
  banner and all branding rasters are regenerated from a rewritten
  `scripts/generate-branding.sh` that uses the NASA Artemis
  Graphic Standards Guide palette (Dec 16 2021) with a seamless
  Cool Horizon Visual background and wide NASA-style tracking.
  Wordmark is now centered on the canvas via a trim + optical
  offset. `--artemis-earth` normalised to the exact spec `#27AAE0`.

### Fixed

- **Daemon plist/service PATH (PR #47).** The summarizer worker
  shells out to the `claude` CLI to generate per-turn recaps, but
  the generated launchd plist and systemd unit only injected HOME —
  no PATH — so launchd fell back to `/usr/bin:/bin:/usr/sbin:/sbin`
  which usually does not contain `~/.local/bin/claude` or
  `/opt/homebrew/bin/claude`. Every recap job was silently failing
  with "executable file not found in $PATH" and the dashboard
  showed no recaps for any turn. Fix: snapshot the install-time
  `os.Getenv("PATH")` in the new `applyDefaultEnv()` helper and
  bake it into both the plist's EnvironmentVariables and the
  systemd unit's Environment= line. A fallback list covers
  stripped shells. Tests: 3 new hermetic tests for the helper plus
  an updated golden plist. Existing installs: run
  `apogee daemon install --force && apogee daemon restart` to pick
  up the new unit file.
- **Banner horizon seam (PR #44).** The PR #43 render_cool_horizon
  composited a transparent radial glow onto a flat NASA Blue field
  with a southeast offset, leaving an uncovered strip along the
  bottom-right edge that read as a hard horizontal seam in the
  README banner. Replaced with a single seamless radial gradient
  on a 2×-tall canvas, cropped to the top half so the brightest
  point sits on the bottom edge. No compositing, no seam.
- **Desktop review polish (PR #39).** Follow-up fixes from
  Copilot's review of PR #35 (Wails desktop shell): wraps
  `StartBackground()` in `sync.Once` so double-start is safe,
  guards `store.Close()` in a sync.Once + deferred fallback so
  an early wails.Run error does not leak the DuckDB lock, uses
  the sentinel `"in-process (wails webview)"` as the HTTPAddr
  for the desktop process so `/v1/info` does not render an empty
  row, and corrects the `make desktop-*` comments to match what
  the targets actually do.

## [0.1.7] - 2026-04-15

### Added

- **PR #37 — Datadog-style facets, timeseries histogram, and perf pass.**
  The `/events` page is rewritten into a triple-panel Datadog Log
  Explorer layout: a collapsible `FacetPanel` on the left with
  `source_app` / `hook_event` / `severity` / `session` groups (each
  showing distinct values + counts that auto-refresh as filters are
  applied), a stacked-bar `LogHistogram` above the table (click-drag
  to zoom into a time range, severity-coloured segments, 80 px tall,
  pure-SVG with no chart-library overhead), and a filter bar with
  free-text body search + a time-range dropdown (`15m` / `1h` / `6h` /
  `24h` / `7d`). The existing cursor-paginated `EventList` now sits
  under the histogram and accepts the shared filter payload so
  Prev/Next still works. All panel state is URL-backed
  (`?q=`, `?window=`, `?since=`, `?until=`, `?facets.<key>=a,b`,
  `?page=N`) so deep links reproduce the exact filter.

  Three new collector endpoints back the rewrite.
  `GET /v1/events/facets` returns the top 50 distinct values + counts
  for each of the four facet dimensions matching the supplied filter;
  one DuckDB `GROUP BY` per dimension so the whole call finishes in
  single-digit ms on a 1M-row `logs` table. `GET /v1/events/timeseries`
  returns evenly spaced buckets with per-severity breakdown via
  DuckDB's `time_bucket()`; the bucket width auto-scales with the
  window (1 min → 1 s, 1 h → 30 s, 24 h → 10 min, 7 d → 1 h).
  `GET /v1/live/bootstrap` is a new consolidated first-paint payload
  for the `/` Live dashboard — replaces the previous 7 parallel
  fetches (`/v1/turns/active`, `/v1/attention/counts`,
  `/v1/events/recent`, and four `/v1/metrics/series`) with one
  response carrying `{ recent_turns, attention, recent_events,
  metrics: { active_turns, tools_rate, errors_rate, hitl_pending },
  now }`. On a warm DuckDB the landing page first paint drops from
  the old ~600 ms (bounded by round-trip serialisation) to ~80 ms
  (one round trip + one paint).

  `internal/store/duckdb/logs.go` gains the supporting primitives:
  a shared `LogFilter.buildWhere()` that understands multi-select
  `SourceApps`/`HookEvents`/`Severities`/`Sessions`, an explicit
  `Since`/`Until` time range, and a free-text `Query`; the existing
  `SourceApp`/`Type`/`SessionID` singular fields are folded into their
  plural counterparts for backward compatibility. New helpers
  `EventFacets`, `EventTimeseries`, and `CountEvents` drive the three
  endpoints above; `ListRecentLogs` is re-plumbed onto the same
  canonicalisation pass so every filter-backed endpoint behaves
  identically. Tests in `internal/store/duckdb/logs_facets_test.go`
  cover facet counts under multi-select, timeseries severity
  breakdown, and the total-count header helper.

  **Performance fixes.** Three separate contention bottlenecks the
  user flagged as "laggy" are addressed together. First,
  `internal/ingest/reconstructor.go` swaps its single global
  `sync.Mutex` for a sharded lock: 16 `reconstructorShard` buckets
  keyed by an FNV-1a 32-bit hash of `session_id`, with a separate
  read/write lock protecting only the top-level sessions map. Apply
  holds only the session's shard for the hot path, so up to 16
  unrelated sessions can ingest events concurrently — previously
  they serialised behind one mutex. `CloseHITLSpan` and the OTel
  span-event mirror are re-plumbed to look up the owning session by
  id instead of ranging over `r.sessions`, which is no longer safe
  under sharding. Second, a new `turnCounterDebouncer`
  (`internal/ingest/turn_counters.go`) coalesces per-turn counter
  writes through a 250 ms quiet window: a tool-heavy turn that
  previously wrote 50-100 `UPDATE turns` rows per turn (one for each
  Pre/PostToolUse pair) now flushes 1-5 terminal writes, with no
  user-visible latency because the dashboard polls active turns at
  2 s. The debouncer cancels pending flushes on `closeTurn` so the
  terminal `"completed"`/`"stopped"` status write is never clobbered
  by a stale `"running"` flush firing after the turn ended.
  Third, `web/app/lib/sse.tsx` maintains `byType` and `bySession`
  indexes alongside the 500-event ring buffer, so `useEventStream`
  with a session filter reads an O(1) precomputed array instead of
  scanning the full history on every render — tangible win when 10+
  consumers with different filters are mounted under the same
  `SSEProvider`.
- **PR #36 — cross-cutting SideDrawer + session summary display.**
  Every dashboard table that used to throw the operator away from the
  current page now opens a Datadog-style detail drawer in place. Clicking
  a row in `/agents`, `/sessions`, the session detail Turns tab, or the
  turn detail span tree slides a side drawer in from the right edge with
  the entity bundle (no navigation, no full reload). The drawer's
  identity lives in the URL via `?drawer=agent|session|turn|span&id=…`,
  so deep links work and the browser back button cleanly closes the
  drawer. Plain left-click pops the drawer; `Cmd+Click` /
  `Shift+Click` / middle-click / right-click still open the matching
  full page in a new tab so power users can keep both views side by
  side. Recursive navigation (clicking a related entity inside one
  drawer) calls the URL-state hook in place — the panel stays mounted
  and only its contents swap, so there is no flash. New TypeScript
  primitives under `web/app/components/`: `SideDrawer` (extended only
  with new presentational siblings — backwards compatible with PR #30's
  callers), `DrawerHeader` + `DrawerTabBar`, `DrawerKeyValue` +
  `DrawerSection`, `DrawerFooterAction`, plus the four entity drawers
  `AgentDrawer`, `SessionDrawer`, `TurnDrawer`, `SpanDrawer`. The
  cross-cutting drawer is mounted exactly once at the root layout via
  `CrossCuttingDrawer.tsx`, which reads `useDrawerState()`
  (`web/app/lib/drawer.tsx`) and dispatches to the right body. A new
  `SessionLabel` component replaces every raw `sess-…` text with the
  short id, source app, and a one-line headline pulled lazily from
  `/v1/sessions/:id/summary` (shared SWR cache key, so multiple rows
  pointing at the same session share one network request). The Go
  collector adds two read-only aggregate routes — `GET
  /v1/agents/:id/detail` and `GET /v1/spans/:trace_id/:span_id/detail`
  — that compute their payloads entirely from the existing `spans` and
  `turns` tables (no schema migration). The collector tests gain two
  new integration cases that exercise the new endpoints against the
  bundled hook samples. See [`docs/drawer.md`](docs/drawer.md) for the
  full design.
- **PR #38 — Watchdog anomaly detection + header bell.** A new
  background worker (`internal/watchdog/`) reads `metric_points` every
  60 s, computes a rolling 24 h baseline mean + stddev for each
  monitored metric, and writes a row to a new `watchdog_signals` table
  whenever the latest 60 s window deviates by more than 3 standard
  deviations. Severity tiers (`info`/`warning`/`critical`) come from
  `|z|` thresholds at 3 / 5 / 8. The detector dedupes by spell — a
  signal is only re-emitted after the metric has stayed below `|z|<1.5`
  for at least 3 minutes — so the bell never alert-storms while a
  metric is still anomalous. The worker is wired into
  `internal/collector/server.go` next to the existing summarizer / hitl
  / interventions services. Two new HTTP routes serve the dashboard:
  `GET /v1/watchdog/signals?status=unacked&limit=N` returns recent
  signals newest-first, and `POST /v1/watchdog/signals/:id/ack` flips
  the `acknowledged` flag (idempotent). Signals broadcast over SSE as
  the new `watchdog.signal` event so the UI updates without polling.
  A new `WatchdogBell` component lives in the TopRibbon between the
  language picker and the theme toggle: it shows a red badge with the
  unread count, pulses when there is at least one critical signal
  (disabled by `prefers-reduced-motion`), and opens a `WatchdogDrawer`
  built on the `SideDrawer` primitive. The drawer renders one card per
  signal — severity icon, headline, label chips, a recharts sparkline
  with the baseline mean reference line, and an Acknowledge button.
  Adds 16 tests across `internal/watchdog/zscore_test.go`,
  `internal/watchdog/watchdog_test.go`,
  `internal/store/duckdb/watchdog_signals_test.go`, and the collector
  integration test in `internal/collector/server_test.go`.
- **PR #39 — menubar as a macOS login item + onboard integration.**
  `apogee menubar` gains three sibling subcommands — `install`,
  `uninstall`, `status` — that register the menu bar companion as a
  **second** launchd unit under `dev.biwashi.apogee.menubar`, written
  to `~/Library/LaunchAgents/dev.biwashi.apogee.menubar.plist`. The
  unit is independent from the collector daemon unit
  (`dev.biwashi.apogee`) and uses a menubar-specific plist shape:
  `LSUIElement=true` (menu-bar-only Cocoa app, no Dock icon),
  `LimitLoadToSessionType=Aqua` (only load under a real GUI login),
  `RunAtLoad=true`, `KeepAlive=false` (interactive — "Quit menubar"
  stays quit until next login), `ProcessType=Interactive`, and
  separate log files at `~/.apogee/logs/menubar.{out,err}.log`. The
  `internal/daemon` package exposes a new `NewManagerWithLabel(label
  string)` constructor and a `daemon.MenubarConfig()` helper so every
  `Install / Uninstall / Start / Stop / Restart / Status` code path
  is reused with the second label. The `apogee onboard` wizard gains
  a `Menubar` group that prompts "Install menubar app as a login
  item?", defaulted to **Install** on a fresh mac and **Re-install**
  when the plist already exists. Non-darwin platforms hide the group
  entirely and the subcommands print a styled warn line
  ("macOS only — apogee menubar is not supported on linux") and exit
  0 so the subcommand tree is discoverable from `--help` everywhere.
  The wizard applies the menubar install **after** the main daemon
  install with partial-success semantics: if the menubar fails but
  the daemon succeeded, the failure is logged and the wizard
  continues — the collector is load-bearing, the menubar is a
  convenience, and rolling back a successful daemon install to undo
  a cosmetic failure would be user-hostile. Adds a
  `dev.biwashi.apogee.menubar` plist template test (LSUIElement,
  LimitLoadToSessionType, KeepAlive=false, menubar log basenames)
  and a cobra-wiring test for the install / uninstall / status
  command tree with a fake manager.
- **PR #35 — static model catalog + probe + dropdowns.** The
  summarizer's three model aliases (recap / rollup / narrative) are now
  chosen from a curated static catalog
  (`internal/summarizer/models.go::KnownModels`) instead of hardcoded
  config defaults. A new probe (`internal/summarizer/models_probe.go`)
  exercises every `current` catalog entry via `claude -p --model <alias>`
  in parallel (concurrency cap 4, 5s per-model timeout) and caches the
  result in a new `model_availability` DuckDB table (24h TTL). A new
  `GET /v1/models` route serves the merged catalog + defaults so the
  Settings page and `apogee onboard` wizard can render proper
  dropdowns — free-text inputs are gone. `validatePreferencesPatch`
  now checks catalog membership via `summarizer.FindModel` and points
  at `/v1/models` in the error message instead of matching a regex.
  The worker resolver order is now `preference > config > cheapest-
  available catalog entry`, and `summarizer.Default()` no longer
  populates `RecapModel` / `RollupModel` / `NarrativeModel` — they
  stay as *explicit TOML overrides* for operators who pin a specific
  alias. `apogee onboard` loads the cache + resolver defaults at
  `loadOnboardState` time and renders three `huh.NewSelect` rows with
  a "Use default (Haiku 4.5)" first entry; probed-unavailable models
  are filtered out of the list. The web Settings page gains a
  `ModelDropdownRow` that renders a native `<select>`, dims
  probed-unavailable entries, shows a "currently unavailable" warning
  pill when the persisted override points at one, and links to the
  raw `/v1/models` JSON via a "View all models →" affordance. Adds
  60+ tests across the catalog resolver, the probe
  concurrency/timeout paths, the DuckDB cache round-trip and prune,
  the HTTP handler's stale-cache refresh path, and the onboard wizard's
  `modelOptions` helper.

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
