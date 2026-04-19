# Phase narrative (summarizer tier 3)

The apogee summarizer ships four layers plus a per-agent worker:

| Tier | Model            | Scope                     | Worker                                     |
| ---- | ---------------- | ------------------------- | ------------------------------------------ |
| 1    | Haiku            | one turn                  | `internal/summarizer/worker.go`            |
| 2    | Sonnet           | one session               | `internal/summarizer/rollup.go`            |
| 3    | Sonnet (default) | one session               | `internal/summarizer/narrative.go`         |
| 4    | Haiku            | one session (live)        | `internal/summarizer/live_status.go`       |
| +    | Haiku            | `(agent_id, session_id)`  | `internal/summarizer/agent_summary.go`     |

**Tier 3 vs tier 4.** Both are session-scoped but they answer different
questions. Tier 3 is a *retrospective* phase narrative — it only looks at
closed turns, chains off the tier-2 rollup, and produces the durable
`phases[]` / `forecast[]` blob the Mission git graph renders. Tier 4 is a
*live* worker — it fires on every span insert while a session is running,
produces a single `"currently <verb>-ing <noun>"` blurb for the Sessions
catalog, and never looks at closed turns.

Tier 3 reads an existing tier-2 rollup and the session's closed turns and
groups them into a small set of semantic *phases* — short, human-readable
chunks that describe the big-picture work being done. The output is an
array of `PhaseBlock` values (plus an optional `forecast[]` array of
predicted upcoming phases) written back onto the same
`session_rollups.rollup_json` row; old rollups without `phases[]` still
parse cleanly.

The Mission tab on `/session?id=<id>` renders each phase as a clickable
node on a vertical git graph spine with a side-drawer full detail
(newest phase on top, operator-intervention branches, TodoWrite plan
rows, `forecast[]` as a dashed tail). Clicking a phase node opens the
drawer. The Live page embeds the same MissionMap component for the
focused session below the triage grid.

## Phase schema

```go
type PhaseBlock struct {
    Index        int            `json:"index"`
    StartedAt    time.Time      `json:"started_at"`
    EndedAt      time.Time      `json:"ended_at"`
    Headline     string         `json:"headline"`     // commit-message style
    Narrative    string         `json:"narrative"`    // 1-3 sentences
    KeySteps     []string       `json:"key_steps"`    // 2-5 items
    Kind         string         `json:"kind"`         // see enum below
    TurnIDs      []string       `json:"turn_ids"`
    TurnCount    int            `json:"turn_count"`
    DurationMs   int64          `json:"duration_ms"`
    ToolSummary  map[string]int `json:"tool_summary"` // e.g. {"Edit":8,"Bash":3}
}
```

`Kind` is one of:

```
implement | review | debug | plan | test | commit | delegate | explore | other
```

The narrative worker also stamps two metadata fields on the outer
`Rollup` blob:

- `narrative_generated_at` — when the tier-3 worker last wrote the phases.
- `narrative_model` — the model alias used.

## Trigger paths

1. **Chained from the tier-2 rollup worker.** When a session rollup
   completes, the service enqueues a narrative job with reason
   `session_rollup` so phases land immediately after. This is the
   default path — the operator never has to click anything.
2. **Manual.** `POST /v1/sessions/:id/narrative` enqueues a job with
   reason `manual`. The Mission tab's `Re-chart` action and the empty-
   state "Generate narrative" button both hit this route. While the
   worker is running, the frontend captures the current
   `narrative_generated_at` as a baseline and renders a spinner plus
   an elapsed-seconds counter (with a 150 s safety timeout) until the
   baseline advances. The SSE `session.updated` broadcast at the tail
   of `process()` is the low-latency signal; the existing 10 s SWR
   poll on `/v1/sessions/:id/rollup` is the fallback.

## Staleness

The narrative worker skips jobs when:

- The session has fewer than 2 closed turns.
- An existing `narrative_generated_at` is within 30 s of now.
- The tier-2 rollup has not changed since the last narrative run, and
  the job reason is not `manual`.

These three guards keep the worker idle on healthy sessions without an
explicit scheduler.

## Prompt shape

`BuildNarrativePrompt` serialises:

- Session metadata (id, source_app, turn count, started_at, last_ended_at)
- The tier-2 rollup (headline + narrative + highlights) as context
- An ordered list of turns with per-turn headline, outcome, key steps,
  and a tool summary derived from spans

followed by the instruction block (English by default, Japanese when
`summarizer.language` is `ja`). The TypeScript schema block stays
English so the model always sees the canonical type definition.

If `summarizer.narrative_system_prompt` is set, it is prepended to the
instruction block under a `# User system prompt` heading.

## Preferences

Three new keys, all scoped under `summarizer.`:

| Key                                  | Type              | Default                 |
| ------------------------------------ | ----------------- | ----------------------- |
| `summarizer.narrative_system_prompt` | string (≤ 2048 B) | `""`                    |
| `summarizer.narrative_model`         | model alias       | `claude-sonnet-4-6`     |
| `summarizer.language`                | `"en"` \| `"ja"`  | inherited from tier 1/2 |

They follow the same resolution order as the existing recap / rollup
preferences: UI override → TOML config → default.

## Cost estimate

One narrative run is one Sonnet call per session, sent whenever the
tier-2 rollup lands. A typical session (5–15 closed turns) produces a
prompt in the 4–10 KB range and an output in the 1–3 KB range — call
it 5 k input + 2 k output tokens per run. At Sonnet pricing that is a
fraction of a cent per session, and the staleness guards keep the worker
idle when nothing has changed.

## Manual regenerate

```sh
curl -X POST http://127.0.0.1:4400/v1/sessions/<id>/narrative
```

The response is `202 Accepted` with `{"enqueued": true}`. The worker
broadcasts a `session.updated` SSE event when the phases land so the
Mission tab revalidates its SWR cache automatically. The frontend
keeps its generation UI in the "generating" state until either
`narrative_generated_at` advances (normal path), the 150 s safety
timeout expires (error state with a Retry button), or the POST itself
fails.
