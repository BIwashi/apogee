# apogee architecture

This document describes the end-to-end architecture of apogee at v0.1.3. It is
the "how everything fits together" reference; the individual subsystems each
have their own document ([`daemon.md`](daemon.md), [`interventions.md`](interventions.md),
[`hooks.md`](hooks.md), [`data-model.md`](data-model.md),
[`otel-semconv.md`](otel-semconv.md), [`cli.md`](cli.md)) and the source code
is the final source of truth. When this doc disagrees with the code, the code
wins — please open a PR.

---

## 30-second pitch

apogee is a single Go binary that:

1. Accepts hook events from every Claude Code session on the machine via the
   `apogee hook` subcommand wired into `.claude/settings.json`.
2. Reconstructs each hook stream into OpenTelemetry-shaped traces (one
   Claude Code user turn = one trace), persists them to an embedded DuckDB
   database, and mirrors the spans to an optional OTLP exporter.
3. Fans live updates out to the embedded Next.js dashboard over SSE.
4. Drives four LLM summarizer tiers (per-turn recap, per-session rollup,
   tier-3 phase narrative, tier-4 live-status) plus a per-agent summary
   worker through the local `claude` CLI.
5. Accepts operator interventions — free-form messages that the next hook
   firing relays back into the live Claude Code session as a hook decision.
6. Runs as a background service (launchd on macOS, systemd `--user` on Linux)
   so the collector starts at every login and never has to be babysat.

Everything ships in one binary. The web UI is embedded via `embed.FS`, so
`brew install BIwashi/tap/apogee` or a release tarball gives you the whole
product with zero Node or Python dependency.

---

## Pipeline

```
Claude Code ──► apogee hook --event X ──► POST /v1/events ──► ingest ──► reconstructor ──► duckdb
                        │                                         │                            │
                        │                                         ├── attention engine ────────┤
                        │                                         ├── summarizer (recap/rollup/narrative/live-status/agent-summary)
                        │                                         ├── interventions.Service ───┤
                        │                                         ├── hitl.Service ────────────┤
                        │                                         └── sse.Hub ─────────────────┤
                        │                                                                      │
                        │                                                metrics collector ────┤
                        │                                                otel exporter ────────┘
                        │
                        └─── POST /v1/sessions/{id}/interventions/claim ◄── operator composer (web UI)

collector serves:
  /v1/events                             hook ingest (writes trigger the pipeline above)
  /v1/events/stream                      SSE fan-out (every state change is broadcast here)
  /v1/turns/active                       live triage roster
  /v1/turns/recent                       recently-closed turn list
  /v1/turns/{turn_id}                    single-turn detail
  /v1/turns/{turn_id}/spans              span tree for a turn
  /v1/turns/{turn_id}/logs               raw hook log tail for a turn
  /v1/turns/{turn_id}/attention          attention state + reasoning for a turn
  /v1/turns/{turn_id}/recap              GET / POST (regenerate) the per-turn recap
  /v1/sessions/recent                    recent session list
  /v1/sessions/search                    fuzzy search across sessions
  /v1/sessions/active                    fleet view: one row per running session (sessions-centric triage)
  /v1/sessions/attention/counts          attention bucket histogram keyed on sessions (not turns)
  /v1/sessions/{id}                      single-session detail
  /v1/sessions/{id}/summary              denormalized header card
  /v1/sessions/{id}/turns                turn list for a session
  /v1/sessions/{id}/logs                 raw hook log tail for a session
  /v1/sessions/{id}/rollup               GET / POST (regenerate) the per-session rollup
  /v1/sessions/{id}/narrative            POST regenerate the tier-3 phase narrative
  /v1/sessions/{id}/todos                latest TodoWrite payload parsed as a typed checklist
  /v1/attention/counts                   scoped attention bucket histogram
  /v1/metrics/series                     scoped KPI series for the sparkline strip
  /v1/filter-options                     sidebar scopes (source_app, model, ...)
  /v1/hitl                               list HITL events
  /v1/hitl/{hitl_id}                     single HITL event
  /v1/hitl/{hitl_id}/respond             operator HITL response
  /v1/sessions/{id}/hitl/pending         pending HITL per session
  /v1/turns/{turn_id}/hitl               HITL events scoped to a turn
  /v1/interventions                      POST submit (operator) / body is the intervention spec
  /v1/interventions/{id}                 GET single
  /v1/interventions/{id}/cancel          POST cancel
  /v1/interventions/{id}/delivered       POST hook-side delivered callback
  /v1/interventions/{id}/consumed        POST reconstructor-side consumed callback
  /v1/sessions/{id}/interventions        GET list-for-session
  /v1/sessions/{id}/interventions/pending GET pending-for-session
  /v1/sessions/{id}/interventions/claim  POST atomic "give me one" (hook side)
  /v1/turns/{turn_id}/interventions      GET pending-for-turn
  /v1/agents/recent                      recent agents (main + subagent) with invocation counts + LLM title/role
  /v1/agents/{id}/detail                 agent bundle for the cross-cutting drawer (agent + parent + children + turns + tool_counts + summary)
  /v1/agents/{id}/summarize              POST re-run the per-agent Haiku title/role worker
  /v1/insights/overview                  aggregate analytics landing page
  /v1/info                               collector build metadata
  /v1/telemetry/status                   OTel exporter config + counters
  /v1/healthz                            liveness probe with an optional JSON body

apogee subcommands:
  apogee serve                           run the collector + embedded dashboard
  apogee init                            write apogee hook entries into .claude/settings.json
  apogee hook --event <X>                the Claude Code hook entry point (binary = hook)
  apogee daemon {install,uninstall,start,stop,restart,status}
                                         launchd / systemd --user supervisor
  apogee status                          one-shot daemon + HTTP liveness probe
  apogee logs                            tail the daemon log files
  apogee open                            open http://127.0.0.1:4100 in the browser
  apogee uninstall                       stop the daemon, strip hooks, optionally purge data
  apogee menubar                         macOS status-bar app (requires the daemon)
  apogee doctor                          health check for PATH, config, and dependencies
  apogee version                         print build version + commit + build time
```

The binary is also the Claude Code hook, so there is no separate hook script
runtime. There is no Python dependency, no embedded hooks filesystem, and no
`hooks/` directory to install. `apogee init` writes the absolute path of the
currently-running binary plus `hook --event X --server-url ...` into
`.claude/settings.json`, and that is the entire install.

---

## The "one user turn = one OTel trace" model

apogee treats a single Claude Code user turn as the unit of observability. A
turn is defined as the span from `UserPromptSubmit` (the user sent a prompt)
to `Stop` (the agent reached its end of turn). Every tool call, subagent run,
and HITL request inside that window is modelled as a child span of the root:

```
trace = claude_code.turn                 (root, opens at UserPromptSubmit, closes at Stop)
├── span  claude_code.tool.Bash
├── span  claude_code.tool.Read
├── span  claude_code.subagent.Explore   (subagent child)
│   ├── span  claude_code.tool.Grep
│   └── span  claude_code.tool.Read
├── span  claude_code.hitl.permission    (stays open until a human responds)
├── span  claude_code.turn.recap         (post-hoc enrichment, linked to the root)
└── event claude_code.notification
```

This gives every trace-shaped tool (Jaeger, Tempo, Honeycomb, Datadog APM,
etc.) a sensible thing to render: a flame graph with the user prompt as the
root and every tool call as a labelled child. Where apogee diverges from a
classic tracing backend is that the dashboard also reads from the denormalized
`turns` and `sessions` tables for fast catalog-style reads that would be
painful to reconstruct from raw spans on every page load.

---

## Subsystems

### `cmd/apogee` — entry point

[`cmd/apogee/main.go`](../cmd/apogee/main.go) is the CLI entry point. It
delegates straight to [`internal/cli/root.go`](../internal/cli/root.go) which
wires up the full cobra command tree. The binary is also the Claude Code hook
(see [`hooks.md`](hooks.md)) and the supervisor client (see
[`daemon.md`](daemon.md)).

### `internal/ingest` — hook → OTel spans

[`internal/ingest/reconstructor.go`](../internal/ingest/reconstructor.go) is
the stateful hook-to-OTel reconstructor. It keeps a per-session agent stack
and a pending `tool_use_id` map so it can parent each `PostToolUse` under the
right subagent and correctly open / close tool spans. Every handler writes to
the store, mirrors the span to OTel via
[`internal/ingest/otelmirror.go`](../internal/ingest/otelmirror.go), and
publishes an SSE envelope to the hub.

### `internal/store/duckdb` — persistence

DuckDB lives in-process via `github.com/marcboeker/go-duckdb/v2`. The schema
is declared in
[`internal/store/duckdb/schema.sql`](../internal/store/duckdb/schema.sql):

| Table | Purpose |
|---|---|
| `sessions` | one row per Claude Code session. Session-scoped attention + fleet-view columns (`attention_state`, `attention_reason`, `attention_score`, `current_turn_id`, `current_phase`, `live_state`, `live_status_text`, `live_status_at`, `live_status_model`) are bubble-written by the reconstructor's `bubbleSessionLive` helper after every turn-level attention rescore, and by the tier-4 live-status worker. |
| `turns` | one row per user turn (= trace root), includes derived `attention_*`, `phase_*`, `recap_json` |
| `spans` | OTel-shaped spans; `attributes_json`, `events_json` carry the rest |
| `logs` | one log per hook event (the raw hook log, lossless) |
| `metric_points` | OTel metric data, write-optimized columnar |
| `hitl_events` | HITL lifecycle rows |
| `session_rollups` | per-session narrative digest (Sonnet tier-2 rollup; tier-3 narrative worker appends `phases[]` + `forecast[]` into the same JSON blob) |
| `agent_summaries` | per-`(agent_id, session_id)` LLM-generated title + role + summary blob from the agent-summary worker (Haiku). Keeps parallel main agents in the same session visually distinct on `/agents`. |
| `interventions` | operator-initiated messages; queued → claimed → delivered → consumed |
| `task_type_history` | rolling per-tool-signature success/failure counts used by the watchlist bucket |
| `watchdog_signals` | anomaly detections emitted by the background watchdog worker |
| `model_availability` | cache of which Claude model aliases the local `claude` CLI accepts |
| `user_preferences` | generic K/V table for summarizer language, system prompts, model overrides, and UI knobs |

See [`data-model.md`](data-model.md) for every column and which subsystem
writes vs reads it.

### `internal/attention` — attention engine

The rule-based attention engine assigns every running turn to one of four
buckets, evaluated on every reconstructor write:

```
healthy ──► watchlist ──► watch ──► intervene_now
```

- **healthy** — nothing unusual. No tool errors, no pending HITL, no staleness.
- **watchlist** — the tool signature of the turn has historically been slow or
  error-prone. Read from `task_type_history` so early warnings are possible
  before anything has gone wrong in the current turn.
- **watch** — real signal: a retry, a pending HITL response older than a few
  seconds, a long-running bash command. The operator should glance.
- **intervene_now** — tool error, blocked-tool event, HITL pending past a hard
  deadline, or an `intervention_pending` signal from the interventions service.
  The dashboard surfaces these at the top of the live triage list.

State transitions are written back onto the `turns` row
(`attention_state`, `attention_reason`, `attention_score`, `attention_tone`)
and broadcast as `turn.updated` SSE events so the dashboard re-sorts without a
refetch.

### `internal/summarizer` — four-tier LLM summarizer + per-agent worker

apogee never talks to the Anthropic API directly. It shells out to the local
`claude` CLI for every LLM call, so the operator's existing auth, rate limits,
and context apply.

- **Tier 1 — recap worker** — per-turn, Haiku. Fires on `Stop`. Produces a
  structured recap JSON (`headline`, `key_steps`, `outcome`, `failure_cause`,
  ...) that is written back onto the `turns.recap_json` column and emitted as
  a post-hoc `claude_code.turn.recap` OTel enrichment span linked to the turn
  root.
- **Tier 2 — rollup worker** — per-session, Sonnet. Fires on `SessionEnd` and
  on a scheduled background cadence. Produces a narrative digest written to
  `session_rollups.rollup_json` (`headline`, `narrative`, `highlights`,
  `patterns`, `open_threads`). The dashboard's session detail page reads from
  this row. See [`narrative.md`](narrative.md) for the chaining contract.
- **Tier 3 — narrative worker** — per-session, Sonnet. Chains off the tier-2
  rollup; groups every closed turn into semantic phases (implement / review /
  debug / plan / test / commit / delegate / explore / other) and produces
  `phases[]` + `forecast[]` appended to the same `session_rollups` JSON blob.
  Manual trigger: `POST /v1/sessions/:id/narrative`. The Mission git graph on
  `/session` renders from this output.
- **Tier 4 — live-status worker** — per-session, Haiku. Fires on every span
  insert with a 10 s debounce (`Config.LiveStatusDebounce`). Produces a
  one-line "currently <verb>-ing <noun>" blurb written to
  `sessions.live_status_text` so the Sessions catalog and Live triage rail
  can describe each running terminal without opening it. Guarded by
  `sessions.live_state = 'live'` so idle sessions do not burn Haiku calls.
- **Agent-summary worker** — per-`(agent_id, session_id)`, Haiku. Enqueue
  sources are: session end (collector fires
  `EnqueueAgentSummariesForSession` on `SessionEnd`), tier-2 rollup
  completion (the summarizer service re-scans the session for candidate
  agents after a rollup lands), and manual refresh via
  `POST /v1/agents/{id}/summarize`. Inside `process()` the worker applies
  two skip guards: the agent's invocation count must have grown past
  `invocation_count_at_generation` *or* the existing summary must be
  older than the 5-minute staleness floor. Produces a structured
  title/role/focus blob written to the `agent_summaries` table. The
  `/agents` catalog leads with this title instead of the literal
  `agent_type`.

Every worker shares a feedback-loop guard: the summarizer sets
`APOGEE_HOOK_SKIP=1` on its `claude` subprocess so the apogee hook
short-circuits and does not re-ingest its own telemetry. All workers degrade
gracefully when `claude` is not on `PATH`: they log once and skip, and the
dashboard shows an empty panel rather than a broken UI.

### `internal/hitl` — Human-In-The-Loop service

HITL events are structured records backed by the `hitl_events` table. The
service owns the lifecycle (pending → responded / timeout / expired / error),
the response API (`POST /v1/hitl/{id}/respond`), and the pending-per-session
query that the turn detail page hydrates. See
[`semconv/model/registry.yaml`](../semconv/model/registry.yaml) for the
attribute group.

### `internal/interventions` — Operator Interventions

The reverse direction of HITL. Operators push a free-form message into a
live Claude Code session via the dashboard composer. The next
`PreToolUse` or `UserPromptSubmit` hook on that session atomically claims the
row (`POST /v1/sessions/{id}/interventions/claim`), writes the Claude Code
decision JSON to stdout, and reports back. See
[`interventions.md`](interventions.md) for the full lifecycle.

### `internal/sse` — SSE fan-out hub

[`internal/sse/event.go`](../internal/sse/event.go) defines every event the
hub broadcasts. The hub is in-process with per-client bounded channels and a
graceful slow-consumer drop. Events include:

- `turn.started`, `turn.updated`, `turn.ended`
- `span.inserted`, `span.updated`
- `session.updated`
- `hitl.*` lifecycle transitions
- `intervention.submitted`, `intervention.claimed`, `intervention.delivered`,
  `intervention.consumed`, `intervention.expired`, `intervention.cancelled`

All events share the same envelope (`{type, at, data}`) so the web client can
dispatch on a single field.

### `internal/metrics` — background metrics sampler

A low-rate background sampler writes OTel-shaped metric points to the
`metric_points` table every few seconds. Used by the KPI sparkline strip on
the Live page and the Insights overview.

### `internal/otel` + `internal/telemetry` — OTLP export

Every reconstructor span is mirrored to a real OTel span via the Go SDK and,
if an OTLP endpoint is configured, exported over OTLP/gRPC or OTLP/HTTP. The
`claude_code.*` namespace is described by
[`semconv/model/registry.yaml`](../semconv/model/registry.yaml) and surfaced
as typed Go constants in
[`semconv/attrs.go`](../semconv/attrs.go). See [`otel-semconv.md`](otel-semconv.md)
for the full attribute table.

Resolution order for telemetry config is **env > TOML > defaults**. When no
endpoint is configured the collector installs a noop tracer provider — the
reconstructor still calls `Tracer.Start` but nothing is exported.

### `internal/daemon` — launchd / systemd supervisor

`apogee daemon {install, uninstall, start, stop, restart, status}` writes a
platform-specific unit file and shells out to `launchctl` (macOS) or
`systemctl --user` (Linux) to manage the process. The unit is always just
`apogee serve --addr 127.0.0.1:4100 --db ~/.apogee/apogee.duckdb` under the
covers — there is nothing special about the daemon process compared to a
foreground `apogee serve`. See [`daemon.md`](daemon.md) for the full
cheatsheet.

### `internal/cli/menubar_darwin.go` — macOS menu bar app

`apogee menubar` is a `caseymrm/menuet` status-bar app that polls the local
collector every 5s and shows daemon + session counts in the menu bar. It
requires the daemon (or a foreground `apogee serve`) to be running. See
[`menubar.md`](menubar.md).

### `internal/webassets` + `web/` — embedded dashboard

The Next.js dashboard is statically exported and embedded into the Go binary
via `embed.FS`. The chi router has an SPA fallback handler that rewrites
every unmatched GET to `index.html`. The routes exposed by the app are:

```
/              Live focus dashboard (flame graph + triage rail)
/sessions      Service catalog (search + filter)
/session?id=   Single-session detail (rollup + per-turn list)
/turn?sess=&turn= Single-turn detail (swim lane + recap + HITL + operator queue)
/agents        Per-agent main/subagent view
/insights      Aggregate analytics (error rate, percentile latency, top tools / phases)
/settings      Collector info, OTel exporter status, install flows
/styleguide    Design token reference (dev only)
```

The command palette (`⌘K`) is a global overlay, not a route. The old
`/timeline` alias was removed in PR #24.

---

## Data flow walkthrough — one tool call

```
Claude Code PreToolUse
        │
        ▼
  apogee hook --event PreToolUse
        │   ├── POST /v1/sessions/{id}/interventions/claim
        │   │     204: pass through stdin → stdout
        │   │     200: write Claude Code decision JSON to stdout, POST /delivered
        │   └── POST /v1/events (always)
        ▼
  ingest.Reconstructor.Apply
        │   ├── opens the turn root span if this is a fresh turn
        │   ├── opens a claude_code.tool.<name> span
        │   ├── writes the span + log row to DuckDB
        │   ├── mirrors the span to the OTel SDK
        │   ├── re-runs the attention engine for the turn
        │   └── broadcasts span.inserted + turn.updated on the SSE hub
        ▼
  web/app (Live page)
        │   ├── receives the envelopes over /v1/events/stream
        │   └── re-renders the flame graph, phase header, and triage rail
        ▼
  PostToolUse lands → the span closes, duration is backfilled, attention
  rescored, and a span.updated envelope fans out the same way.
```

Every write is append-only — DuckDB is safe to read concurrently during a
burst without locking the writer — and every side channel (OTel, SSE,
summarizer, attention engine) runs off the same "one hook event in, one state
transition out" contract so failures in one side-channel never block the
others.

---

## Constraints

- **Single binary.** No sidecar databases, no embedded JRE, no Node runtime at
  deploy time. DuckDB is in-process, the Next.js bundle is embedded.
- **Local-first.** Works with zero network access. OTLP export is optional.
- **Never break Claude Code.** The hook exits 0 on every error. Transport
  failures log to stderr and are swallowed. A failing hook would break Claude
  Code, which is the exact opposite of what an observability tool should do.
- **Short-lived storage.** The DuckDB database is designed for dev-loop
  observability, not long-term archival. Expect to rotate or export to
  Parquet when the file grows past a few GB.
- **Dark-first UI.** See [`design-tokens.md`](design-tokens.md).

---

## Background service story

On a fresh machine the full setup is:

```sh
brew install BIwashi/tap/apogee
apogee init                  # write hooks into ~/.claude/settings.json
apogee daemon install        # register launchd / systemd --user unit
apogee daemon start          # launch it now; it will also start on every login
apogee open                  # opens http://127.0.0.1:4100 in the browser
```

On macOS the same machine can optionally run the menu bar app:

```sh
apogee menubar &
```

The menu bar polls the daemon's HTTP surface and renders a compact status in
the status bar. See [`menubar.md`](menubar.md).

To remove apogee entirely:

```sh
apogee uninstall             # stops the daemon, strips hooks, prompts before deleting data
apogee uninstall --purge     # also wipes ~/.apogee
```

---

## Where to read next

- [`cli.md`](cli.md) — every subcommand and flag
- [`hooks.md`](hooks.md) — hook contract and wire shapes
- [`interventions.md`](interventions.md) — operator-initiated messages
- [`daemon.md`](daemon.md) — launchd / systemd supervisor
- [`menubar.md`](menubar.md) — macOS status-bar app
- [`data-model.md`](data-model.md) — DuckDB schema reference
- [`otel-semconv.md`](otel-semconv.md) — `claude_code.*` attributes
- [`design-tokens.md`](design-tokens.md) — visual system spec
