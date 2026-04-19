# Cross-cutting SideDrawer

PR #36 introduces a Datadog-style row-click pattern across every dashboard
table. Clicking a row in `/agents`, `/sessions`, the session detail Turns
tab, or the turn detail span tree slides a detail drawer in from the right
edge of the viewport. The current page never unmounts, so the operator
keeps their context — filters, scroll position, and SSE subscriptions stay
exactly where they were.

## URL contract

The drawer's identity lives entirely in the query string so deep links
work, the browser back button closes the drawer, and recursive navigation
inside the drawer never flashes a new panel.

```
?drawer=agent&drawer_id=<agent_id>
?drawer=session&drawer_id=<session_id>
?drawer=turn&drawer_sess=<session_id>&drawer_turn=<turn_id>
?drawer=span&drawer_trace_id=<trace_id>&drawer_span_id=<span_id>
```

Every drawer query parameter is namespaced under `drawer_*` so the
cross-cutting drawer can sit on routes that already use plain `id` /
`sess` / etc. for their own state (for example
`/session?id=…&drawer=agent&drawer_id=…`).

Reload `/sessions?drawer=agent&drawer_id=ag-1234` and the agent drawer pops on
top of the sessions catalog, ready to be shared with a teammate. Press
`Esc` or click the backdrop and the drawer closes via `router.replace()`
without reloading the page.

## Row click vs full page

Every row that opens a drawer is also wrapped in an anchor pointing at the
full page. Plain left-click pops the drawer (`event.preventDefault()`).
`Cmd+Click`, `Shift+Click`, middle-click, and right-click → "Open in new
tab" continue to work — the dashboard never traps an operator who wants to
keep the current view side-by-side with the full detail.

## Session labels

Raw `sess-…` strings are meaningless on their own. Every table that shows
a session id renders the new `SessionLabel` component instead, which adds
the source app and a one-line headline pulled from
`/v1/sessions/:id/summary`. Multiple rows that point at the same session
share one SWR cache key so the network only sees one request per id.

## Backend

Two read-only routes plus one write-trigger route feed the drawers:

- `GET /v1/agents/:id/detail` — returns the agent row plus its parent and
  direct children, the last 20 turns the agent participated in, and a
  per-tool histogram of its span activity. The `agent` field on the
  response now carries the LLM-generated `title` / `role` /
  `summary_model` / `summary_at` fields pulled from `agent_summaries`.
- `POST /v1/agents/:id/summarize` — enqueues a manual run of the per-
  agent Haiku title/role worker. Returns `202 Accepted`; the drawer's
  Summary section revalidates via the `session.updated` SSE broadcast.
- `GET /v1/spans/:trace_id/:span_id/detail` — returns a single span plus
  its parent (nil for trace roots) and direct children so the drawer's
  Parent tab can render click-through navigation in one round-trip.

Both read endpoints aggregate over the existing `spans`, `turns`, and
the new `agent_summaries` table; no schema migration is required on
top of PR #100's additions.

## AgentDrawer summary section

PR #100 adds a Summary section at the top of the AgentDrawer that leads
with the agent's LLM-generated title, renders the role as a secondary
line, and shows the generator metadata (`generated_at`, `model`) next to
a `Regenerate` button that hits `POST /v1/agents/:id/summarize`. The
`/agents` catalog mirrors this — each row leads with the `title`
instead of the literal `agent_type` so parallel main agents in the same
session stop looking identical.
