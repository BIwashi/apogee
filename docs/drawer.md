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

Two new read-only routes feed the drawers:

- `GET /v1/agents/:id/detail` — returns the agent row plus its parent and
  direct children, the last 20 turns the agent participated in, and a
  per-tool histogram of its span activity.
- `GET /v1/spans/:trace_id/:span_id/detail` — returns a single span plus
  its parent (nil for trace roots) and direct children so the drawer's
  Parent tab can render click-through navigation in one round-trip.

Both endpoints are pure aggregates over the existing `spans` and `turns`
tables; no schema migration is required.
