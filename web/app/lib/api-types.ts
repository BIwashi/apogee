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

/**
 * RecapOutcome mirrors internal/summarizer.RecapOutcome. Tile order also
 * implies UI severity tone (success > partial > aborted > failure).
 */
export type RecapOutcome = "success" | "partial" | "failure" | "aborted";

export interface RecapPhase {
  name: string;
  start_span_index: number;
  end_span_index: number;
  summary: string;
}

/**
 * Recap mirrors internal/summarizer.Recap. Populated by the Haiku-powered
 * summariser worker after every turn closes.
 */
export interface Recap {
  headline: string;
  outcome: RecapOutcome;
  phases: RecapPhase[];
  key_steps: string[];
  failure_cause: string | null;
  notable_events: string[];
  generated_at: string;
  model: string;
  prompt_tokens?: number;
  output_tokens?: number;
}

export interface RecapResponse {
  recap: Recap | null;
  generated_at?: string;
  model?: string;
}

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

/**
 * LogRow mirrors internal/store/duckdb.LogRow. Used by the turn-detail and
 * session-detail raw-log panels.
 */
export interface LogRow {
  id: number;
  timestamp: string;
  trace_id?: string;
  span_id?: string;
  severity_text: string;
  severity_number: number;
  body: string;
  session_id?: string;
  turn_id?: string;
  hook_event: string;
  source_app: string;
  attributes?: Record<string, unknown>;
}

/**
 * PhaseSegment mirrors internal/attention.PhaseSegment. The collector emits
 * these alongside spans on /v1/turns/:id/spans so the swim lane can colour
 * the phase row without re-implementing the heuristic in TypeScript.
 */
export interface PhaseSegment {
  name: Phase | string;
  started_at: string;
  ended_at: string;
}

export interface TurnSpansResponse {
  spans: Span[];
  phases: PhaseSegment[];
}

export interface SessionTurnsResponse {
  turns: Turn[];
}

export interface TurnLogsResponse {
  logs: LogRow[];
}

export interface SessionLogsResponse {
  logs: LogRow[];
}

/**
 * AttentionSignal is one piece of evidence the engine considered while
 * scoring a turn. Mirrors internal/attention.Signal.
 */
export interface AttentionSignal {
  kind: string;
  value?: unknown;
  threshold?: unknown;
  weight: number;
}

/**
 * AttentionDetail is the response shape of GET /v1/turns/:id/attention.
 * Surfaces the engine's stored decision plus the full signal slice so the
 * turn-detail page can explain *why* a turn is flagged.
 */
export interface AttentionDetail {
  turn_id: string;
  state?: AttentionState | string;
  tone?: AttentionTone | string;
  reason?: string;
  score?: number;
  phase?: Phase | string;
  signals: AttentionSignal[];
  updated_at: string;
}

/**
 * SpanTreeNode is the recursive in-page projection of a Span with its
 * children attached. Built on the client from the flat span list using
 * parent_span_id.
 */
export type SpanTreeNode = Span & { children: SpanTreeNode[] };

/**
 * TurnDetail bundles every payload the turn-detail page renders into one
 * convenience type. Components inside the page receive narrower slices.
 */
export interface TurnDetail {
  turn: Turn;
  spans: Span[];
  phases: PhaseSegment[];
  logs: LogRow[];
  attention: AttentionDetail | null;
}

/**
 * FilterKey is the union of swim-lane / span-tree filter chips. Persisted in
 * the URL `?filter=` query param so deep links are shareable.
 */
export type FilterKey =
  | "all"
  | "commands"
  | "messages"
  | "tools"
  | "errors"
  | "hitl"
  | "subagents";

export const FILTER_KEYS: FilterKey[] = [
  "all",
  "commands",
  "messages",
  "tools",
  "errors",
  "hitl",
  "subagents",
];
