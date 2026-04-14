# apogee architecture

This is a forward-looking sketch. The scaffold PR only lays out the monorepo
and the design system; the collector, store, SSE fan-out, OpenTelemetry
bridge, and embedded web UI land in subsequent PRs. Use this doc as the plan
of record and keep it in sync as the pieces land.

---

## 30-second pitch

apogee is a single Go binary that:

1. Accepts POSTed hook events from Claude Code (via Python reference hooks).
2. Stores them in an embedded DuckDB database.
3. Fans them out live to a Next.js dashboard over SSE.
4. Optionally emits every event as an OTLP span using `claude_code.*`
   semantic conventions.

Everything ships in one binary. The web UI is embedded via `embed.FS` so
`go install` gets you the whole product.

---

## Component diagram

```
                        ┌────────────────────────────────────────┐
                        │         apogee binary (Go)             │
                        │                                        │
  ┌────────────┐        │  ┌───────────┐     ┌───────────────┐   │
  │ Claude     │ POST   │  │ HTTP (chi)│     │ SSE fan-out   │   │
  │ Code +     ├────────┼─▶│ /ingest   ├────▶│ /events/stream│──┐│
  │ Python     │        │  └────┬──────┘     └───────────────┘  ││
  │ hooks      │        │       │                                ││
  └────────────┘        │       ▼                                ││
                        │  ┌────────────┐    ┌──────────────┐    ││
                        │  │ store      │───▶│ DuckDB file  │    ││
                        │  │ (append +  │    │ ./data/*.db  │    ││
                        │  │  query)    │    └──────────────┘    ││
                        │  └────┬───────┘                        ││
                        │       │                                ││
                        │       ▼                                ││
                        │  ┌────────────┐                        ││
                        │  │ otel bridge│──▶ OTLP/HTTP (optional)││
                        │  └────────────┘                        ││
                        │                                        ││
                        │  ┌─────────────────────────────┐       ││
                        │  │ embedded Next.js UI (FS)    │◀──────┘│
                        │  │ GET /                       │        │
                        │  │ GET /_next/*                │        │
                        │  └─────────────────────────────┘        │
                        └────────────────────────────────────────┘
                                         │
                                         ▼
                               ┌────────────────────┐
                               │   Browser (UI)     │
                               │  SSE + SWR + d3    │
                               └────────────────────┘
```

---

## Modules (planned)

### `cmd/apogee`
Single entry point. Subcommands:
- `apogee serve` — run the collector + embedded UI (default).
- `apogee version` — print the build version.
- `apogee migrate` — run DuckDB schema migrations.
- `apogee export` — dump events to Parquet or NDJSON.

### `internal/version`
Build version string. `-ldflags "-X .../version.Version=..."` will override
the default `0.0.0-dev` during release builds.

### `internal/collector` (future)
Chi-based HTTP server. Routes:
- `POST /ingest` — receives one hook event.
- `GET  /api/v1/sessions`
- `GET  /api/v1/sessions/{id}`
- `GET  /api/v1/events`
- `GET  /events/stream` — SSE stream of live events.

### `internal/store` (future)
Thin wrapper around DuckDB. Append-only writes, window-function reads. The
store owns the schema and migrations. We will pin the DuckDB Go driver
version and build tags in the PR that introduces it.

### `internal/sse` (future)
In-process fan-out hub. The ingest handler writes one event to the store and
publishes it to the hub; the hub pushes to every connected SSE client with a
per-client bounded channel and graceful slow-consumer drop.

### `internal/otel` (future)
Optional OTLP/HTTP exporter. Every ingest event becomes a span using the
semantic conventions defined in `semconv/`. When `APOGEE_OTLP_ENDPOINT` is
unset this module is a no-op.

### `internal/webui` (future)
Holds the `embed.FS` that contains the Next.js standalone build output. The
HTTP server serves `/` and `/_next/*` from it.

### `semconv/`
OpenTelemetry semantic conventions for `claude_code.*` attributes — session
id, agent id, event type, tool name, etc. This is the schema contract
between apogee and any downstream OTel collector.

### `hooks/`
Python reference hooks that Claude Code users drop into their
`~/.claude/hooks/` directory. They POST to `/ingest` with the hook payload.

### `web/`
Next.js 16 dashboard. App Router, Tailwind v4, SWR for REST, native
EventSource for SSE. Pages planned:
- `/` — Overview (live pulse, session grid)
- `/timeline` — global timeline across sessions
- `/sessions` — session list
- `/sessions/[id]` — session detail with swim-lane + transcript
- `/agents` — per-agent rollups
- `/settings` — local preferences

---

## Data flow (a single tool call)

```
Claude Code
   │  PreToolUse
   ▼
Python hook (~/.claude/hooks/pre_tool_use.py)
   │  POST /ingest  { "type": "PreToolUse", ... }
   ▼
apogee collector (HTTP)
   │  1. append to DuckDB          (internal/store)
   │  2. publish to SSE hub        (internal/sse)
   │  3. export as OTLP span       (internal/otel, optional)
   ▼
Browser
   │  EventSource /events/stream receives event
   ▼
React UI (SWR + d3) renders into timeline, pulse chart, session swim-lane
```

Every write is append-only, so the store is safe to query concurrently
during a burst without locking the writer.

---

## Constraints

- **Single binary.** No sidecar databases, no embedded JRE, no Node runtime
  at deploy time. DuckDB is in-process, the Next.js bundle is embedded.
- **Local-first.** Works with zero network access. OTLP export is optional.
- **Low cardinality.** Avoid unbounded labels in OTel attributes; prefer
  hashing long values.
- **Short lived.** The collector is meant to run alongside a dev loop; the
  store is not a time-series database. Plan for periodic compaction and
  eventual export to Parquet.

---

## What is NOT in the scaffold PR

- Any Go code beyond `version` and `main`
- DuckDB dependency
- Chi / HTTP server
- SSE fan-out
- OTLP exporter
- Python hooks
- Embedded FS
- `/timeline`, `/sessions`, `/agents`, `/settings` routes

Every item above gets its own PR. This doc will grow alongside them.
