# apogee OpenTelemetry semantic conventions

apogee owns a small `claude_code.*` semantic convention namespace that
describes Claude Code execution. The authoritative source lives in
[`semconv/model/registry.yaml`](../semconv/model/registry.yaml); the
Go constants in [`semconv/attrs.go`](../semconv/attrs.go) mirror that
file one-to-one and are unit-tested for drift.

This document is a human-readable index. When you add or rename an
attribute, edit the YAML, regenerate the constants by hand, and run
`go test ./semconv/...` to verify the two stay in sync.

## Span names

| Name | Kind | Notes |
| --- | --- | --- |
| `claude_code.turn` | server | One per Claude Code user turn. Trace root. |
| `claude_code.tool.<name>` | internal | One per tool call. `<name>` is the Claude Code tool name. |
| `claude_code.tool.mcp.<server>.<tool>` | internal | MCP-provided tool calls. |
| `claude_code.subagent.<type>` | internal | One per subagent run. Children of the turn root. |
| `claude_code.hitl.permission` | internal | One per HITL permission request. |
| `claude_code.turn.recap` | internal | Post-hoc enrichment span emitted when the summarizer lands a recap. Linked to the turn root. |

## Resource attributes

apogee always emits the standard OTel resource keys plus the apogee-
specific overrides supplied via `OTEL_RESOURCE_ATTRIBUTES` or
`[telemetry.resource]` in the TOML config.

| Key | Type | Notes |
| --- | --- | --- |
| `service.name` | string | Defaults to `apogee`. |
| `service.version` | string | Build version from `internal/version`. |
| `service.instance.id` | string | `<hostname>-<pid>` per process. |

## Attribute groups

### `claude_code.session.*`

| Attribute | Type | Required | Notes |
| --- | --- | --- | --- |
| `claude_code.session.id` | string | required | Stable Claude Code session UUID. |
| `claude_code.session.source_app` | string | recommended | Logical source application that owns the session. |
| `claude_code.session.machine_id` | string | opt-in | Originating machine identifier. |
| `claude_code.session.model` | string | recommended | Model alias active at the time of the event. |

### `claude_code.turn.*`

| Attribute | Type | Notes |
| --- | --- | --- |
| `claude_code.turn.id` | string | UUIDv7 turn id. |
| `claude_code.turn.status` | enum | `running` \| `completed` \| `errored` \| `stopped` \| `compacted`. |
| `claude_code.turn.prompt_chars` | int | Length of the user prompt. |
| `claude_code.turn.output_chars` | int | Length of the assistant output. |
| `claude_code.turn.tool_call_count` | int | Number of tool calls inside the turn. |
| `claude_code.turn.subagent_count` | int | Number of subagents spawned. |
| `claude_code.turn.error_count` | int | Number of failed tool calls. |

### `claude_code.agent.*`

| Attribute | Type | Notes |
| --- | --- | --- |
| `claude_code.agent.id` | string | Stable agent identifier. |
| `claude_code.agent.kind` | enum | `main` \| `subagent`. |
| `claude_code.agent.parent_id` | string | Parent agent for a subagent. |
| `claude_code.agent.type` | string | Subagent type (`Explore`, `Plan`, ...). |

### `claude_code.tool.*`

| Attribute | Type | Notes |
| --- | --- | --- |
| `claude_code.tool.name` | string | Tool name as advertised by Claude Code. |
| `claude_code.tool.use_id` | string | Tool use id from the assistant message. |
| `claude_code.tool.mcp.server` | string | MCP server name. |
| `claude_code.tool.mcp.name` | string | MCP tool name. |
| `claude_code.tool.input_summary` | string | Compact rendering of input args. |
| `claude_code.tool.output_summary` | string | Compact rendering of output payload. |
| `claude_code.tool.blocked_reason` | string | Reason a tool call was blocked by policy/HITL. |

### `claude_code.phase.*`

| Attribute | Type | Notes |
| --- | --- | --- |
| `claude_code.phase.name` | enum (open) | `plan`, `explore`, `edit`, `test`, `run`, `commit`, `debug`, `delegate`, `verify`, `idle`. |
| `claude_code.phase.inferred_by` | enum | `heuristic` \| `llm` \| `agent_declared`. |

### `claude_code.hitl.*`

| Attribute | Type | Notes |
| --- | --- | --- |
| `claude_code.hitl.id` | string | HITL request id (`hitl-<8 hex>`). |
| `claude_code.hitl.kind` | enum | `permission` \| `tool_approval` \| `prompt` \| `choice`. |
| `claude_code.hitl.status` | enum | `pending` \| `responded` \| `timeout` \| `error` \| `expired`. |
| `claude_code.hitl.decision` | enum | `allow` \| `deny` \| `custom` \| `timeout`. |
| `claude_code.hitl.reason_category` | string | Coarse reason category. |
| `claude_code.hitl.operator_note` | string | Free-form note. |
| `claude_code.hitl.resume_mode` | enum | `continue` \| `retry` \| `abort` \| `alternative`. |

### `claude_code.attention.*`

| Attribute | Type | Notes |
| --- | --- | --- |
| `claude_code.attention.state` | enum | `healthy` \| `watchlist` \| `watch` \| `intervene_now`. |
| `claude_code.attention.score` | double | Numeric score in `[0, 1]`. |
| `claude_code.attention.reason` | string | Short rationale. |

### `claude_code.recap.*`

| Attribute | Type | Notes |
| --- | --- | --- |
| `claude_code.recap.headline` | string | One-line headline. |
| `claude_code.recap.outcome` | enum | `success` \| `partial` \| `failure` \| `aborted`. |
| `claude_code.recap.model` | string | Recap model alias. |
| `claude_code.recap.key_steps` | string[] | Ordered list of key steps. |
| `claude_code.recap.failure_cause` | string | Free-form failure description. |

### `claude_code.compaction.*`

| Attribute | Type | Notes |
| --- | --- | --- |
| `claude_code.compaction.trigger` | string | Trigger that initiated the compaction. |

## Events

| Event | Notes |
| --- | --- |
| `claude_code.prompt` | User prompt text on the turn root. Carries `claude_code.prompt.text` and `claude_code.prompt.chars`. |
| `claude_code.assistant_message` | Assistant message during the turn. Carries `claude_code.assistant_message.text` and `claude_code.assistant_message.chars`. |
| `claude_code.notification` | A Claude Code notification surfaced during a turn. Carries `claude_code.notification.type` and `claude_code.notification.message`. |
| `claude_code.tool.blocked` | A tool call was blocked. Carries `claude_code.tool.blocked_reason`. |

## GenAI semconv reuse

apogee also writes the upstream `gen_ai.*` attributes on every span
that carries a model or token usage:

- `gen_ai.system` — set to `anthropic`.
- `gen_ai.request.model`
- `gen_ai.response.model`
- `gen_ai.operation.name`
- `gen_ai.usage.input_tokens`
- `gen_ai.usage.output_tokens`

These keys are defined by the OpenTelemetry GenAI semconv SIG; apogee
does not own them. Reusing them lets generic OTel backends (Tempo,
Jaeger, Datadog, Honeycomb, ...) render Claude Code traces with their
existing GenAI dashboards.
