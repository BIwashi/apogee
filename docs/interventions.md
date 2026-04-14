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
  "operator_id": "shota",
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

The Python hook library under `hooks/apogee_intervention.py` owns the
client half:

1. On `PreToolUse` and `UserPromptSubmit`, `send_event.py` calls
   `apogee_intervention.handle_hook(...)` *before* POSTing the hook event
   to `/v1/events`.
2. The helper calls `POST /v1/sessions/<sid>/interventions/claim` with the
   hook event and (optional) turn id.
3. On 204 the helper returns `None` and the hook stays in pass-through mode
   — echo stdin to stdout and POST the hook event normally.
4. On 200 it formats the Claude Code decision JSON — either `{"decision":
   "block","reason":"..."}` (PreToolUse) or
   `{"hookSpecificOutput":{"additionalContext":"..."}}` (UserPromptSubmit)
   — and writes it to stdout in place of the stdin echo.
5. It then calls `POST /v1/interventions/<id>/delivered` so the collector
   flips the row to `delivered` and broadcasts
   `intervention.delivered` over SSE.

Every network call is best-effort. Errors are logged to stderr and
swallowed so Claude Code's hook pipeline is never broken.

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
