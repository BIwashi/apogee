/**
 * TypeScript mirrors of the Go types served by the collector. Field names
 * stay in snake_case so they can be consumed without any runtime mapping.
 * Keep this file aligned with:
 *   internal/store/duckdb/sessions.go  (Session)
 *   internal/store/duckdb/turns.go     (Turn)
 *   internal/store/duckdb/spans.go     (SpanRow)
 *   internal/store/duckdb/filter_options.go (FilterOptions)
 *   internal/sse/event.go              (Event envelope + payloads)
 */

export interface Session {
  session_id: string;
  source_app: string;
  started_at: string;
  ended_at?: string;
  last_seen_at: string;
  turn_count: number;
  model?: string;
  machine_id?: string;
}

export type TurnStatus =
  | "running"
  | "completed"
  | "stopped"
  | "errored"
  | "compacted";

/**
 * AttentionState mirrors internal/attention.State. Ordered from most urgent
 * to least — intervene_now first.
 */
export type AttentionState =
  | "intervene_now"
  | "watch"
  | "watchlist"
  | "healthy";

export const ATTENTION_STATES: AttentionState[] = [
  "intervene_now",
  "watch",
  "watchlist",
  "healthy",
];

export type AttentionTone =
  | "critical"
  | "warning"
  | "info"
  | "success"
  | "muted";

/** Phase mirrors internal/attention.Phase. */
export type Phase =
  | "delegating"
  | "testing"
  | "committing"
  | "editing"
  | "exploring"
  | "running"
  | "idle";

export interface Turn {
  turn_id: string;
  trace_id: string;
  session_id: string;
  source_app: string;
  started_at: string;
  ended_at?: string;
  duration_ms?: number;
  status: TurnStatus | string;
  model?: string;
  prompt_text?: string;
  prompt_chars?: number;
  output_chars?: number;
  tool_call_count: number;
  subagent_count: number;
  error_count: number;
  input_tokens?: number;
  output_tokens?: number;
  headline?: string;
  outcome_summary?: string;
  attention_state?: AttentionState | string;
  attention_reason?: string;
  attention_score?: number;
  attention_tone?: AttentionTone | string;
  phase?: Phase | string;
  phase_confidence?: number;
  phase_since?: string;
}

export interface AttentionCounts {
  intervene_now: number;
  watch: number;
  watchlist: number;
  healthy: number;
  total: number;
}

export interface MetricSeriesPoint {
  at: string;
  value: number;
}

export interface MetricSeries {
  name: string;
  window: string;
  step: string;
  kind: string;
  points: MetricSeriesPoint[];
}

export interface Span {
  trace_id: string;
  span_id: string;
  parent_span_id?: string;
  name: string;
  kind: string;
  start_time: string;
  end_time?: string;
  duration_ns?: number;
  status_code: string;
  status_message?: string;
  service_name: string;
  session_id?: string;
  turn_id?: string;
  agent_id?: string;
  agent_kind?: string;
  tool_name?: string;
  tool_use_id?: string;
  mcp_server?: string;
  mcp_tool?: string;
  hook_event?: string;
  attributes?: Record<string, unknown>;
  events?: unknown[];
}

export interface FilterOptions {
  source_apps: string[];
  hook_events: string[];
  tool_names: string[];
  agent_ids: string[];
}

/** SSE wire type constants. Must match internal/sse/event.go. */
export const SSE_EVENT_TYPES = {
  Initial: "initial",
  TurnStarted: "turn.started",
  TurnUpdated: "turn.updated",
  TurnEnded: "turn.ended",
  SpanInserted: "span.inserted",
  SpanUpdated: "span.updated",
  SessionUpdated: "session.updated",
} as const;

export type SSEEventType =
  (typeof SSE_EVENT_TYPES)[keyof typeof SSE_EVENT_TYPES];

/** Discriminated payload shapes keyed off Event.type. */
export interface TurnPayload {
  turn: Turn;
}

export interface SpanPayload {
  span: Span;
}

export interface SessionPayload {
  session: Session;
}

export interface InitialPayload {
  recent_turns: Turn[];
  recent_sessions: Session[];
}

/**
 * Event envelope. `Data` is the decoded payload; the generic defaults to
 * `unknown` so consumers narrow on `type` before touching it.
 */
export interface ApogeeEvent<Data = unknown> {
  type: SSEEventType | string;
  at: string;
  data: Data;
}

/** Tagged union for convenience when iterating over a mixed history. */
export type AnyApogeeEvent =
  | ApogeeEvent<InitialPayload>
  | ApogeeEvent<TurnPayload>
  | ApogeeEvent<SpanPayload>
  | ApogeeEvent<SessionPayload>;

export interface RecentTurnsResponse {
  turns: Turn[];
}

export interface RecentSessionsResponse {
  sessions: Session[];
}
