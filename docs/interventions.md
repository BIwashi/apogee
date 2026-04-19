# Operator Interventions

apogee already runs a Human-In-The-Loop pipeline where Claude Code asks for
permission and the operator answers. **Operator interventions** are the
reverse direction: the operator pushes a message *into* a live Claude Code
session, and the next hook firing on that session returns the message to the
agent as a Claude Code hook decision.

Interventions never break Claude Code — everything is routed through the
existing `.claude/settings.json` hooks and the hook script degrades to a
plain pass-through on any failure.

## Lifecycle

```
queued → claimed → delivered → consumed
                            ↘ expired
                            ↘ cancelled
```

- **queued** — the operator submitted via `POST /v1/interventions`; no hook
  has picked it up yet.
- **claimed** — an inbound hook invocation atomically acquired the row via
  `POST /v1/sessions/<session_id>/interventions/claim`. Any other concurrent
  hook sees `claimed` and skips.
- **delivered** — the hook successfully wrote the Claude Code decision JSON
  to stdout and reported back via `POST /v1/interventions/<id>/delivered`.
- **consumed** — the reconstructor observed a downstream hook event
  (e.g. `PostToolUse`, `Stop`) on the same session, which is a best-effort
  proxy that Claude Code processed the block/context injection.
- **expired** — the auto-expire TTL elapsed, or the intervention's scope
  ended (turn closed / session ended).
- **cancelled** — the operator rescinded it while still queued or claimed.

## Delivery modes

| mode       | hook path          | Claude Code output                                           |
| ---------- | ------------------ | ------------------------------------------------------------ |
| `interrupt`| `PreToolUse`       | `{"decision":"block","reason":"<message>"}`                  |
| `context`  | `UserPromptSubmit` | `{"hookSpecificOutput":{"additionalContext":"<message>"}}`   |
| `both`     | either             | whichever hook fires first wins; PreToolUse is preferred     |

`interrupt` is the default because it is the only mode that deliberately
derails the agent mid-action. `context` is for "background" hints that the
agent can consider on its next turn. `both` is the safety net: emit an
interrupt if a tool call is about to happen, else inject additional context
on the next user turn.

## Scope

- **this_turn** — bound to the `turn_id` that was running at the time of
  submission. Expires when that turn closes.
- **this_session** — persists until delivered, cancelled, or until the
  session ends.

`this_turn` is the default because it matches the operator's intent in the
common case ("this run looks wrong, stop it now").

## Urgency

`high`, `normal`, and `low` map onto the attention engine's `intervene_now`,
`watch`, and `healthy` buckets for the turn the intervention is pending on.
The urgency field also drives the claim priority: high interventions jump
the queue past normal and low interventions on the same session.

## Auto-expire TTL

Every intervention carries an `auto_expire_at` timestamp computed as
`created_at + TTL` (default 10 min, tunable in `[interventions]`). A
background sweeper runs every 15 s and flips any non-terminal row whose
deadline has passed to `expired`.

## HTTP API

| method | path                                               | purpose                           |
| ------ | -------------------------------------------------- | --------------------------------- |
| POST   | `/v1/interventions`                                | Operator submits a new intervention (201) |
| GET    | `/v1/interventions/{id}`                           | Fetch a single intervention       |
| POST   | `/v1/interventions/{id}/cancel`                    | Operator cancels (409 on terminal rows) |
| POST   | `/v1/interventions/{id}/delivered`                 | Hook reports delivery             |
| POST   | `/v1/interventions/{id}/consumed`                  | Hook or reconstructor reports consumption |
| POST   | `/v1/sessions/{id}/interventions/claim`            | Atomic "give me one" for hooks (204 when nothing matches) |
| GET    | `/v1/sessions/{id}/interventions`                  | List all interventions for a session |
| GET    | `/v1/sessions/{id}/interventions/pending`          | List non-terminal interventions (hot path) |
| GET    | `/v1/turns/{turn_id}/interventions`                | List pending interventions scoped to a turn |

### Request body for POST /v1/interventions

```json
{
  "session_id": "sess-abc",
  "turn_id": "turn-xyz",
  "message": "Stop, reconsider your approach",
  "delivery_mode": "interrupt",
  "scope": "this_turn",
  "urgency": "normal",
  "operator_id": "op-alice",
  "notes": "",
  "ttl_seconds": 600
}
```

Validation:
- `message` is required, max 4096 characters (configurable).
- `delivery_mode` must be `interrupt | context | both`.
- `scope` must be `this_turn | this_session`. When `this_session` the
  `turn_id` is ignored server-side.
- `urgency` must be `high | normal | low`.

## Hook-side contract

The Go-native `apogee hook` subcommand (implemented in
`internal/cli/hook.go`) owns the client half. The binary itself is the
Claude Code hook entry point — `.claude/settings.json` contains lines
like `apogee hook --event PreToolUse --server-url http://localhost:4100/v1/events`,
and `apogee init` writes them there pointing at the absolute path of
the running apogee binary.

1. On `PreToolUse` and `UserPromptSubmit`, the hook subcommand tries to
   claim an operator intervention *before* POSTing the hook event to
   `/v1/events`.
2. It calls `POST /v1/sessions/<sid>/interventions/claim` with the
   hook event and (optional) turn id. The base URL is the `--server-url`
   value with the trailing `/v1/events` suffix stripped.
3. On 204 the hook stays in pass-through mode — echo stdin to stdout and
   POST the hook event normally.
4. On 200 it formats the Claude Code decision JSON — either `{"decision":
   "block","reason":"..."}` (PreToolUse, mode `interrupt` or `both`) or
   `{"hookSpecificOutput":{"additionalContext":"..."}}` (UserPromptSubmit,
   mode `context` or `both`) — and writes it to stdout in place of the
   stdin echo.
5. It then calls `POST /v1/interventions/<id>/delivered` so the collector
   flips the row to `delivered` and broadcasts
   `intervention.delivered` over SSE.

Every network call is best-effort. Errors are logged to stderr and
swallowed so Claude Code's hook pipeline is never broken, and the hook
subcommand always exits 0 (a failing hook would break Claude Code).

## SSE events

The collector broadcasts one event per state transition on the existing
`/v1/events/stream` channel:

- `intervention.submitted`
- `intervention.claimed`
- `intervention.delivered`
- `intervention.consumed`
- `intervention.expired`
- `intervention.cancelled`

All six share the same wire envelope (`{type, at, data}`) and carry a
`data.intervention` field whose shape matches
`web/app/lib/api-types.ts :: Intervention`.

## Configuration

`~/.apogee/config.toml`:

```toml
[interventions]
auto_expire_ttl_seconds = 600
sweep_interval_seconds = 15
both_fallback_after_seconds = 60
max_message_chars = 4096
```

Every field has an environment-variable override:

- `APOGEE_INTERVENTIONS_TTL_SECONDS`
- `APOGEE_INTERVENTIONS_SWEEP_SECONDS`
- `APOGEE_INTERVENTIONS_BOTH_FALLBACK_SECONDS`
- `APOGEE_INTERVENTIONS_MAX_MESSAGE_CHARS`

Precedence: env > TOML > default (matches the summarizer).

## Failure modes

- **Collector unreachable**: hook logs to stderr, returns pass-through. The
  operator still sees the intervention queued in the dashboard; as soon as
  a later hook lands it is claimed retroactively.
- **Hook claims a row but crashes before `delivered`**: the row sits in
  `claimed` until the auto-expire sweeper flips it to `expired`.
- **Operator cancels while claimed**: the row moves to `cancelled`. A
  later `delivered` POST is rejected with 409.
- **Downstream hook never fires**: the row stays `delivered` until the
  turn closes (at which point the reconstructor flips it via
  `ExpireForTurn`) or the TTL sweeper catches it.

The UI (PR #15) should therefore treat `delivered` as a soft-terminal
state — the operator's message has already been shown to the agent — and
reserve `consumed` for visual confirmation that Claude Code moved past it.

## UI walkthrough

PR #15 ships three React components under `web/app/components/` plus a
composite section that glues them onto the turn detail page.

- **`InterventionComposer`** — the keyboard-first form. Textarea, three
  radio groups (delivery mode / scope / urgency), live character count
  against the 4096-char cap, and a 3px left-border whose hue reflects
  the current urgency. `Ctrl/Cmd+Enter` submits, `Esc` clears. When the
  parent passes `autoFocus` the textarea grabs focus on mount.
- **`InterventionQueue`** — the live card of `queued` / `claimed` /
  `delivered` rows for a session. Pulls
  `/v1/sessions/<id>/interventions/pending` on a 2s SWR interval and
  calls `mutate()` on every matching `intervention.*` SSE event. Each
  row carries a staleness pill: `waiting 45s` at 30s, upgrading to
  `stalled — no hook activity` at 120s.
- **`InterventionTimeline`** — the compact history card of terminal
  rows (`consumed` / `expired` / `cancelled`). Uses the same chip and
  icon language as the queue and collapses to 20 rows by default with
  a `show more` button.
- **`OperatorQueueSection`** — the composite glue. Lays out composer
  and queue side by side (stacking below 1100px) with the timeline
  underneath, and is dropped into `web/app/turn/page.tsx` above the
  existing Recap + HITL grid so operator-initiated actions sit above
  reactive HITL on the page.

On the turn detail page header, a pulsing staleness chip renders next
to the attention dot when any queued intervention on the current turn
exceeds 30s. The chip reads `OPERATOR WAITING · 45s` at warning tone
and upgrades to `NO HOOK ACTIVITY · 2m 12s` at critical tone when the
deadline passes 120s — the explicit surfacing of the idle-session
safety net.

On the session detail page, an **Intervention summary** card on the
Overview tab shows `N queued · M in flight · K lifetime` with an
`Open composer →` link that jumps straight to the most recent running
turn with `?compose=1`. The Turns tab exposes an **Intervene** button
on every running turn that opens the turn detail with the same deep
link.

### Keyboard shortcut

`Alt+I` on the turn detail page opens and focuses the composer
regardless of the current selection. The shortcut is surfaced as a
`kbd` hint next to the `OPERATOR QUEUE` section header.

### Deep links

- `/turn?sess=<sess>&turn=<turn>&compose=1` — opens the turn detail
  with the composer pre-focused.
- `/session?id=<sess>&tab=turns` — jumps to the running-turn list with
  the per-row Intervene buttons.
