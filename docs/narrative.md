# Phase narrative (summarizer tier 3)

The apogee summarizer ships three layers:

| Tier | Model            | Scope        | Worker                          |
| ---- | ---------------- | ------------ | ------------------------------- |
| 1    | Haiku            | one turn     | `internal/summarizer/worker.go` |
| 2    | Sonnet           | one session  | `internal/summarizer/rollup.go` |
| 3    | Sonnet (default) | one session  | `internal/summarizer/narrative.go` |

Tier 3 reads an existing tier-2 rollup and the session's closed turns and
groups them into a small set of semantic *phases* — short, human-readable
chunks that describe the big-picture work being done. The output is an
array of `PhaseBlock` values written back onto the same
`session_rollups.rollup_json` row; old rollups without `phases[]` still
parse cleanly.

The Timeline tab on `/session?id=<id>` renders each phase as a clickable
card with a hover preview and a side-drawer full detail. Clicking a
phase opens the drawer; hovering for 350 ms shows a floating preview
card with the full narrative + key steps + tool summary.

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
   reason `manual`. The Timeline tab's regenerate button and empty-state
   "Generate narrative" button both hit this route.

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
Timeline tab revalidates its SWR cache automatically.
