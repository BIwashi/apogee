# apogee data model

This is the column-level reference for every DuckDB table apogee writes. The
authoritative schema lives in
[`internal/store/duckdb/schema.sql`](../internal/store/duckdb/schema.sql);
migrations are applied automatically on first open.

apogee uses DuckDB as an in-process append-only column store. Writes are
additive (or row-replacing for the rollup / intervention tables); reads are
served directly from the same file that the reconstructor is writing to.
There is no ORM — every query lives under
[`internal/store/duckdb/`](../internal/store/duckdb/).

---

## Entity overview

```
sessions         one row per Claude Code session
  └── turns      one row per user turn (= one OTel trace)
        ├── spans            hook-derived OTel spans parented to the turn
        ├── hitl_events      HITL lifecycle rows scoped to the turn
        ├── interventions    operator messages scoped to the turn or session
        └── logs             raw hook log rows scoped to the turn

session_rollups  one row per session, Sonnet-tier narrative digest
task_type_history rolling success/failure counts keyed by tool signature
metric_points    OTel metric points (time series)
```

---

## `sessions`

One row per Claude Code `session_id` seen by the collector. Written by the
reconstructor whenever a new session id appears; never deleted except via
`apogee uninstall --purge`.

| Column | Type | Purpose |
| --- | --- | --- |
| `session_id` | VARCHAR PK | Claude Code session UUID |
| `source_app` | VARCHAR | Derived label — repo name or pinned value |
| `started_at` | TIMESTAMP | First event timestamp |
| `ended_at` | TIMESTAMP NULL | Set when a `SessionEnd` hook lands |
| `last_seen_at` | TIMESTAMP | Updated on every event |
| `turn_count` | INTEGER | Denormalised count of turns under this session |
| `model` | VARCHAR NULL | Last-seen Claude Code model alias |
| `machine_id` | VARCHAR NULL | Optional machine identifier |

**Writers:** reconstructor (insert on first event, update on every event).
**Readers:** sessions catalog, session detail page, interventions service
(scoping), summarizer rollup worker, command palette.

---

## `turns`

One row per user turn — the unit of a single OTel trace. The turn is opened
at `UserPromptSubmit` and closed at `Stop`. The row carries both raw metadata
and derived columns written back by the attention engine and the summarizer
recap worker.

| Column | Type | Purpose |
| --- | --- | --- |
| `turn_id` | VARCHAR PK | Turn id |
| `trace_id` | VARCHAR UNIQUE | OTel trace id (1:1 with `turn_id`) |
| `session_id` | VARCHAR | Parent session |
| `source_app` | VARCHAR | Derived label copied from the session |
| `started_at` | TIMESTAMP | `UserPromptSubmit` timestamp |
| `ended_at` | TIMESTAMP NULL | `Stop` timestamp |
| `duration_ms` | BIGINT NULL | Derived from end - start |
| `status` | VARCHAR | `running` / `completed` / `errored` / `stopped` / `compacted` |
| `model` | VARCHAR NULL | Claude Code model alias |
| `prompt_text` | VARCHAR | User prompt (truncated) |
| `prompt_chars` | INTEGER | Length of the prompt |
| `output_chars` | INTEGER | Length of the assistant output |
| `tool_call_count` | INTEGER | Number of tool spans |
| `subagent_count` | INTEGER | Number of subagent spans |
| `error_count` | INTEGER | Number of failed tool calls |
| `input_tokens` | BIGINT NULL | Upstream model input tokens |
| `output_tokens` | BIGINT NULL | Upstream model output tokens |
| `headline` | VARCHAR NULL | One-line outcome label (recap) |
| `outcome_summary` | VARCHAR NULL | Short outcome summary (recap) |
| `attention_state` | VARCHAR | `healthy` / `watchlist` / `watch` / `intervene_now` |
| `attention_reason` | VARCHAR | Short rationale |
| `attention_score` | DOUBLE | Numeric score in `[0, 1]` |
| `attention_tone` | VARCHAR | UI tone key |
| `phase` | VARCHAR | `plan` / `explore` / `edit` / `test` / `run` / `commit` / `debug` / `delegate` / `verify` / `idle` |
| `phase_confidence` | DOUBLE | Confidence in the phase heuristic |
| `phase_since` | TIMESTAMP | Timestamp the turn entered the current phase |
| `attention_signals_json` | VARCHAR | JSON blob of the attention engine signals |
| `recap_json` | VARCHAR NULL | Structured recap JSON from the summarizer |
| `recap_generated_at` | TIMESTAMP NULL | When the recap was written |
| `recap_model` | VARCHAR NULL | Model alias that produced the recap |

**Writers:** reconstructor (raw columns), attention engine
(`attention_*`, `phase_*`), summarizer recap worker (`recap_json`,
`recap_generated_at`, `recap_model`, `headline`, `outcome_summary`).

**Readers:** live page flame graph + triage rail, turn detail page,
sessions catalog, insights overview, SSE broadcast builder.

---

## `spans`

OTel-shaped span rows. One row per span, parented by `trace_id` + `parent_span_id`.
`attributes_json` and `events_json` carry the rest of the OTel payload so
non-indexed fields do not pollute the column list.

| Column | Type | Purpose |
| --- | --- | --- |
| `trace_id` | VARCHAR | Part of PK — identifies the trace (= turn) |
| `span_id` | VARCHAR | Part of PK |
| `parent_span_id` | VARCHAR NULL | Parent span, or NULL for the turn root |
| `name` | VARCHAR | `claude_code.turn` / `claude_code.tool.*` / `claude_code.subagent.*` / `claude_code.hitl.permission` / `claude_code.turn.recap` |
| `kind` | VARCHAR | OTel span kind (`INTERNAL` / `SERVER` / ...) |
| `start_time` | TIMESTAMP | Span open time |
| `end_time` | TIMESTAMP NULL | Span close time |
| `duration_ns` | BIGINT NULL | Derived from end - start |
| `status_code` | VARCHAR | `UNSET` / `OK` / `ERROR` |
| `status_message` | VARCHAR NULL | Error description |
| `service_name` | VARCHAR | Defaults to `claude-code` |
| `session_id` | VARCHAR NULL | Denormalized session id |
| `turn_id` | VARCHAR NULL | Denormalized turn id |
| `agent_id` | VARCHAR NULL | Agent id (main or subagent) |
| `agent_kind` | VARCHAR NULL | `main` / `subagent` |
| `tool_name` | VARCHAR NULL | Claude Code tool name |
| `tool_use_id` | VARCHAR NULL | `tool_use_id` from the assistant message |
| `mcp_server` | VARCHAR NULL | MCP server name when applicable |
| `mcp_tool` | VARCHAR NULL | MCP tool name when applicable |
| `hook_event` | VARCHAR NULL | Hook event that opened the span |
| `attributes_json` | VARCHAR | JSON object — all `claude_code.*` + `gen_ai.*` attributes |
| `events_json` | VARCHAR | JSON array — span events (e.g. `claude_code.notification`) |

### `attributes_json` shape

Strict JSON object. Keys are the `claude_code.*` attribute ids from
[`semconv/model/registry.yaml`](../semconv/model/registry.yaml) plus a
handful of upstream `gen_ai.*` keys:

```json
{
  "claude_code.tool.name": "Bash",
  "claude_code.tool.use_id": "tool_01HXYZ...",
  "claude_code.tool.input_summary": "git status",
  "claude_code.phase.name": "test",
  "claude_code.phase.inferred_by": "heuristic",
  "gen_ai.system": "anthropic",
  "gen_ai.request.model": "claude-sonnet-4-6"
}
```

### `events_json` shape

Strict JSON array. Each element is an object with `name`, `time_ms` (unix
millis), and `attributes` (a flat object of string/number/bool values):

```json
[
  { "name": "claude_code.prompt", "time_ms": 1713138123456, "attributes": { "claude_code.prompt.chars": 412 } },
  { "name": "claude_code.notification", "time_ms": 1713138125123, "attributes": { "claude_code.notification.type": "idle" } }
]
```

**Writers:** reconstructor + OTel mirror + summarizer recap span emitter.
**Readers:** turn detail swim lane, span-tree fetch endpoint, attention
engine signal collection.

---

## `logs`

Raw hook log rows — one row per hook event. This is the lossless raw log
that the "Raw logs" panel on the turn detail page renders, and the dataset
that the reconstructor re-reads on a full turn rebuild.

| Column | Type | Purpose |
| --- | --- | --- |
| `id` | BIGINT PK | Auto-increment id |
| `timestamp` | TIMESTAMP | Hook event timestamp |
| `trace_id` | VARCHAR NULL | Trace id when known |
| `span_id` | VARCHAR NULL | Span id when known |
| `severity_text` | VARCHAR | OTel log severity string |
| `severity_number` | INTEGER | OTel log severity number |
| `body` | VARCHAR | Log body (usually the raw hook payload snippet) |
| `session_id` | VARCHAR NULL | Session id |
| `turn_id` | VARCHAR NULL | Turn id |
| `hook_event` | VARCHAR | Hook event name |
| `source_app` | VARCHAR | Derived label |
| `attributes_json` | VARCHAR | JSON object for any extra structured fields |

**Writers:** reconstructor (one row per hook event).
**Readers:** turn detail raw logs panel, session detail raw logs panel.

---

## `metric_points`

OTel metric points, columnar and write-optimised. Written by the background
metrics sampler and by the reconstructor on span close (to record a
histogram of span durations).

| Column | Type | Purpose |
| --- | --- | --- |
| `id` | BIGINT PK | Auto-increment id |
| `timestamp` | TIMESTAMP | Sample timestamp |
| `name` | VARCHAR | Metric name (`claude_code.turn.duration_ms`, ...) |
| `kind` | VARCHAR | `gauge` / `counter` / `histogram` |
| `value` | DOUBLE NULL | Scalar value for counters / gauges |
| `histogram_json` | VARCHAR NULL | Histogram bucket body when `kind=histogram` |
| `unit` | VARCHAR NULL | Unit string |
| `labels_json` | VARCHAR | JSON object of labels |

**Writers:** `internal/metrics/collector.go`, reconstructor.
**Readers:** `/v1/metrics/series` (KPI sparkline), Insights overview.

---

## `hitl_events`

Human-In-The-Loop lifecycle rows. One row per permission / tool_approval /
prompt / choice request. Lifecycle is `pending → responded | timeout |
expired | error`.

| Column | Type | Purpose |
| --- | --- | --- |
| `id` | BIGINT PK | Auto-increment id |
| `hitl_id` | VARCHAR UNIQUE | Stable HITL id (`hitl-<8 hex>`) |
| `span_id` | VARCHAR | HITL span id |
| `trace_id` | VARCHAR | Trace / turn id |
| `session_id` | VARCHAR | Session id |
| `turn_id` | VARCHAR | Turn id |
| `kind` | VARCHAR | `permission` / `tool_approval` / `prompt` / `choice` |
| `status` | VARCHAR | `pending` / `responded` / `timeout` / `expired` / `error` |
| `requested_at` | TIMESTAMP | When the HITL opened |
| `responded_at` | TIMESTAMP NULL | When a human responded |
| `question` | VARCHAR | Question body |
| `suggestions_json` | VARCHAR | JSON array of suggested responses |
| `context_json` | VARCHAR | JSON object for extra context fields |
| `decision` | VARCHAR NULL | `allow` / `deny` / `custom` / `timeout` |
| `reason_category` | VARCHAR NULL | Coarse reason bucket |
| `operator_note` | VARCHAR NULL | Free-form operator note |
| `resume_mode` | VARCHAR NULL | `continue` / `retry` / `abort` / `alternative` |
| `operator_id` | VARCHAR NULL | Who responded |

**Writers:** `internal/hitl/service.go`.
**Readers:** turn detail HITL queue panel, session HITL endpoint, attention
engine signal collection.

---

## `session_rollups`

Per-session narrative digest produced by the summarizer's Sonnet tier. One
row per session; replaced in place (`INSERT OR REPLACE`) whenever a new
rollup lands.

| Column | Type | Purpose |
| --- | --- | --- |
| `session_id` | VARCHAR PK | Session id |
| `generated_at` | TIMESTAMP | When the rollup was written |
| `model` | VARCHAR | Model alias that produced the rollup |
| `from_turn_id` | VARCHAR NULL | First turn covered |
| `to_turn_id` | VARCHAR NULL | Last turn covered |
| `turn_count` | INTEGER | Number of turns covered |
| `rollup_json` | VARCHAR | Narrative digest JSON (shape below) |

The `rollup_json` blob carries the tier-2 narrative plus the optional
tier-3 `phases[]` array:

```jsonc
{
  "headline": "Refactored the daemon core",
  "narrative": "…",
  "highlights": ["…"],
  "patterns": [],
  "open_threads": [],

  // Tier-3 phase narrative (optional, written by internal/summarizer/narrative.go).
  "phases": [
    {
      "index": 0,
      "started_at": "2026-04-15T09:30:00Z",
      "ended_at":   "2026-04-15T09:38:00Z",
      "headline": "Implemented the daemon core",
      "narrative": "Added internal/daemon with launchd/systemd units and tests.",
      "key_steps": ["added launchd unit", "added systemd unit", "tests green"],
      "kind": "implement",
      "turn_ids": ["turn-a", "turn-b"],
      "turn_count": 2,
      "duration_ms": 480000,
      "tool_summary": { "Edit": 8, "Bash": 3 }
    }
  ],
  "narrative_generated_at": "2026-04-15T09:40:12Z",
  "narrative_model": "claude-sonnet-4-6",

  "generated_at": "2026-04-15T09:40:00Z",
  "model": "claude-sonnet-4-6",
  "turn_count": 3
}
```

Old rollups without `phases[]` still parse — the Go `Rollup` struct marks
the field `omitempty` and the TypeScript `Rollup` interface marks it
optional. See [`narrative.md`](narrative.md) for the full tier-3
contract.

**Writers:** `internal/summarizer/rollup.go` (tier 2),
`internal/summarizer/narrative.go` (tier 3).
**Readers:** session detail page Overview tab (rollup panel) and
Timeline tab (phase timeline), sessions catalog tooltip.

---

## `interventions`

Operator-initiated messages pushed into a live Claude Code session. The
lifecycle is
`queued → claimed → delivered → consumed` with `cancelled` / `expired` as
terminal off-ramps. A claim is an atomic flip executed by `apogee hook` at
`PreToolUse` / `UserPromptSubmit` time.

| Column | Type | Purpose |
| --- | --- | --- |
| `intervention_id` | VARCHAR PK | Stable id |
| `session_id` | VARCHAR | Target session |
| `turn_id` | VARCHAR NULL | Target turn (when `scope=this_turn`) |
| `operator_id` | VARCHAR NULL | Who submitted the intervention |
| `created_at` | TIMESTAMP | Submission time |
| `claimed_at` | TIMESTAMP NULL | When a hook claimed it |
| `delivered_at` | TIMESTAMP NULL | When the hook reported delivery |
| `consumed_at` | TIMESTAMP NULL | When a downstream hook observed follow-up activity |
| `expired_at` | TIMESTAMP NULL | When the sweeper flipped it to `expired` |
| `cancelled_at` | TIMESTAMP NULL | When the operator cancelled |
| `auto_expire_at` | TIMESTAMP | Deadline for the sweeper |
| `message` | VARCHAR | Operator message body |
| `delivery_mode` | VARCHAR | `interrupt` / `context` / `both` |
| `scope` | VARCHAR | `this_turn` / `this_session` |
| `urgency` | VARCHAR | `high` / `normal` / `low` |
| `status` | VARCHAR | Current lifecycle state |
| `delivered_via` | VARCHAR NULL | Hook event that delivered it |
| `consumed_event_id` | BIGINT NULL | Log row id that consumed it |
| `notes` | VARCHAR NULL | Free-form operator notes |

**Writers:** `internal/interventions/service.go` (submit / claim /
delivered / consumed / expired / cancelled), `apogee hook` (claim and
delivered callbacks).

**Readers:** operator queue UI, SSE broadcast builder, attention engine
(`intervention_pending` signal).

---

## `task_type_history`

Rolling success / failure counts keyed by the canonical tool signature of a
turn. Populated by the attention engine when a turn closes, and read back
for the `watchlist` bucket — which is how apogee can warn on a tool pattern
that has historically been slow or error-prone *before* anything has gone
wrong in the current turn.

| Column | Type | Purpose |
| --- | --- | --- |
| `pattern` | VARCHAR PK | Canonical tool signature (sorted set of tool names) |
| `turn_count` | BIGINT | Total turns seen with this signature |
| `success_count` | BIGINT | Turns that closed `completed` |
| `failure_count` | BIGINT | Turns that closed `errored` |
| `last_updated` | TIMESTAMP | Last row update |

**Writers:** attention engine on turn close.
**Readers:** attention engine on turn open (watchlist lookup).

---

## `user_preferences`

A generic K/V table for operator-tweakable runtime knobs. Values are
JSON-encoded strings so future preferences can hold richer shapes (lists,
objects) without a schema migration. Owned by PR #29 — the typed accessors
live in [`internal/store/duckdb/preferences.go`](../internal/store/duckdb/preferences.go)
and the HTTP routes in
[`internal/collector/preferences.go`](../internal/collector/preferences.go).

| Column | Type | Purpose |
| --- | --- | --- |
| `key` | VARCHAR PK | Dotted preference id (`summarizer.language`, ...) |
| `value_json` | VARCHAR | JSON-encoded value blob |
| `updated_at` | TIMESTAMP | Last write time, UTC |

Documented summarizer keys (more may be added later without a migration):

| Key | Default | Meaning |
| --- | --- | --- |
| `summarizer.language` | `"en"` | Output language for the recap + rollup workers. `"en"` or `"ja"`. |
| `summarizer.recap_system_prompt` | `""` | Free text appended to the Haiku recap instruction block, max 2048 chars. |
| `summarizer.rollup_system_prompt` | `""` | Free text appended to the Sonnet rollup instruction block, max 2048 chars. |
| `summarizer.recap_model` | `""` | Override for the recap model alias. Empty → fall back to `[summarizer] recap_model` in `~/.apogee/config.toml`. |
| `summarizer.rollup_model` | `""` | Override for the rollup model alias. Empty → fall back to the config file. |

**Writers:** `PATCH /v1/preferences`, `DELETE /v1/preferences`.
**Readers:** the summarizer worker pool reloads at the top of every job so
prompt language and model overrides land without a restart.

---

## Indexing cheat sheet

The schema creates a small set of indexes tuned for the dashboard's hot
paths:

- `sessions(last_seen_at DESC)` — recent-sessions list.
- `sessions(source_app)` — scope filter.
- `turns(session_id, started_at DESC)` — per-session turn list.
- `turns(started_at DESC)` — live and recent turn lists.
- `turns(status)` — active turns.
- `turns(attention_state)` — dashboard sort by attention.
- `spans(trace_id)` — span tree by turn.
- `spans(session_id, start_time)` — session-scoped span queries.
- `spans(turn_id, start_time)` — turn-scoped span queries.
- `spans(tool_name)`, `spans(name)`, `spans(start_time DESC)` — filters
  across all turns.
- `logs(session_id, timestamp)`, `logs(trace_id)`, `logs(hook_event)`,
  `logs(timestamp DESC)` — raw log fetches.
- `hitl_events(session_id, requested_at DESC)`,
  `hitl_events(turn_id)`, `hitl_events(status)`, `hitl_events(kind)`.
- `session_rollups(generated_at DESC)` — recently-generated rollups.
- `interventions(session_id, created_at DESC)`,
  `interventions(session_id, status)` (pending fast path),
  `interventions(status)`, `interventions(auto_expire_at)` (sweeper).
- `task_type_history(last_updated DESC)` — warm cache.
- `metric_points(name, timestamp DESC)` — time series fetches.

---

## Migrations

`internal/store/duckdb/migrate.go` applies column additions on every open by
diffing `PRAGMA table_info` against the target schema. This keeps older
databases compatible with new binaries without requiring an explicit
`apogee migrate` step — the migrator runs as part of `apogee serve`
startup.

**Never hand-edit a DuckDB file.** Rotate or wipe with
`apogee uninstall --purge` if you need a clean slate.
