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

/**
 * PhaseKind mirrors internal/summarizer PhaseKind* constants. Used by the
 * tier-3 narrative worker to classify each semantic phase of a session
 * timeline. The set is intentionally small so the web UI can map each kind
 * to a fixed color + icon.
 */
export type PhaseKind =
  | "implement"
  | "review"
  | "debug"
  | "plan"
  | "test"
  | "commit"
  | "delegate"
  | "explore"
  | "other";

/**
 * PhaseBlock mirrors internal/summarizer.PhaseBlock. One semantic chunk of
 * a session timeline produced by the tier-3 narrative worker. Covers one
 * or more consecutive turns and carries an LLM-written headline, narrative
 * paragraph, and key-step bullets so the web UI can render it as a
 * clickable timeline card.
 */
export interface PhaseBlock {
  index: number;
  started_at: string;
  ended_at: string;
  headline: string;
  narrative: string;
  key_steps: string[];
  kind: PhaseKind;
  turn_ids: string[];
  turn_count: number;
  duration_ms: number;
  tool_summary: Record<string, number>;
}

/**
 * ForecastPhase mirrors internal/summarizer.ForecastPhase. One predicted
 * upcoming phase emitted by the tier-3 narrative LLM in the same call
 * that produces phases[]. No turn indices — forecasts do not correspond
 * to recorded turns yet. The web UI renders these as dimmed dashed
 * planets beyond the realised phase chain on the Mission Map.
 */
export interface ForecastPhase {
  kind: PhaseKind;
  headline: string;
  rationale?: string;
}

/**
 * Rollup mirrors internal/summarizer.Rollup. Populated by the Sonnet-powered
 * session rollup worker; one row per session. The optional `phases` array is
 * populated by the tier-3 narrative worker after the rollup lands.
 */
export interface Rollup {
  headline: string;
  narrative: string;
  highlights: string[];
  patterns: string[];
  open_threads: string[];
  /** Tier-3 phase narrative. Undefined until the narrative worker runs. */
  phases?: PhaseBlock[];
  /** Tier-3 forecast — 0..3 predicted next phases. Undefined pre-tier-3. */
  forecast?: ForecastPhase[];
  /** When the narrative worker last wrote phases. Absent pre-tier-3. */
  narrative_generated_at?: string;
  /** Model alias the narrative worker used. Absent pre-tier-3. */
  narrative_model?: string;
  generated_at: string;
  model: string;
  turn_count: number;
}

export interface RollupResponse {
  rollup: Rollup | null;
  generated_at: string | null;
  model: string | null;
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

export interface TimeRangeOption {
  label: string;
  seconds: number;
}

export interface FilterOptions {
  source_apps: string[];
  session_ids: string[];
  hook_events: string[];
  tool_names: string[];
  time_ranges: TimeRangeOption[];
}

export interface SessionSearchHit {
  session_id: string;
  source_app: string;
  last_seen_at: string;
  turn_count: number;
  latest_headline?: string;
  latest_prompt_snippet?: string;
  attention_state?: string;
}

export interface SessionSearchResponse {
  sessions: SessionSearchHit[];
}

export interface SessionSummary {
  session_id: string;
  source_app: string;
  started_at: string;
  last_seen_at: string;
  ended_at?: string;
  model?: string;
  machine_id?: string;
  turn_count: number;
  running_count: number;
  completed_count: number;
  errored_count: number;
  latest_headline?: string;
  latest_turn_id?: string;
  attention_state?: string;
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
  HITLRequested: "hitl.requested",
  HITLResponded: "hitl.responded",
  HITLExpired: "hitl.expired",
  InterventionSubmitted: "intervention.submitted",
  InterventionClaimed: "intervention.claimed",
  InterventionDelivered: "intervention.delivered",
  InterventionConsumed: "intervention.consumed",
  InterventionExpired: "intervention.expired",
  InterventionCancelled: "intervention.cancelled",
  WatchdogSignal: "watchdog.signal",
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
  | ApogeeEvent<SessionPayload>
  | ApogeeEvent<HITLPayload>
  | ApogeeEvent<InterventionPayload>
  | ApogeeEvent<WatchdogPayload>;

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
 * EventsRecentResponse mirrors the body of GET /v1/events/recent. The
 * collector returns at most `limit` rows ordered newest-first; `next_before`
 * is the cursor (smallest id in the batch) the client passes as `?before=`
 * to fetch the next page. `has_more` is true when the page is full and the
 * client should enable the "Next" button.
 */
export interface EventsRecentResponse {
  events: LogRow[];
  next_before: number | null;
  has_more: boolean;
}

/**
 * FacetValue is one distinct value + row-count pair inside a facet
 * dimension. Mirrors internal/store/duckdb.FacetValue.
 */
export interface FacetValue {
  value: string;
  count: number;
}

/**
 * FacetDimension is a single left-rail column on the /events page.
 * Mirrors internal/store/duckdb.FacetDimension. Values are ordered
 * descending by count and capped at 50 entries.
 */
export interface FacetDimension {
  key: string;
  values: FacetValue[];
}

/**
 * EventFacetsResponse is the body of GET /v1/events/facets. `window` is
 * the caller's ?window= query param (e.g. "15m", "1h", "24h") if supplied.
 */
export interface EventFacetsResponse {
  window?: string;
  since?: string;
  until?: string;
  facets: FacetDimension[];
}

/**
 * EventBucket is one time-series bucket from GET /v1/events/timeseries.
 * Mirrors internal/store/duckdb.EventBucket. `by_severity` is keyed by
 * lowercased severity text ("info", "warn", "error", ...). Severities
 * with zero entries in the bucket are omitted.
 */
export interface EventBucket {
  bucket: string;
  total: number;
  by_severity: Record<string, number>;
}

/**
 * EventTimeseriesResponse is the body of GET /v1/events/timeseries.
 * `total` is the unfiltered row count across every returned bucket —
 * the /events header renders it as "N events found".
 */
export interface EventTimeseriesResponse {
  window?: string;
  since?: string;
  until?: string;
  step: string;
  total: number;
  buckets: EventBucket[];
}

/**
 * LiveBootstrapResponse is the body of GET /v1/live/bootstrap — the
 * consolidated first-paint payload for the Live landing page. PR #37
 * consolidates ~7 parallel fetches into one round trip.
 */
export interface LiveBootstrapResponse {
  recent_turns: Turn[];
  attention: AttentionCounts;
  recent_events: LogRow[];
  metrics: {
    active_turns: MetricSeriesPoint[];
    tools_rate: MetricSeriesPoint[];
    errors_rate: MetricSeriesPoint[];
    hitl_pending: MetricSeriesPoint[];
  };
  now: string;
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
 * HITL types — mirror internal/store/duckdb/hitl.go and internal/sse/event.go.
 * Keep these in sync with the Go side; the wire shape is the
 * SnapshotFromHITL() projection.
 */
export type HITLKind = "permission" | "tool_approval" | "prompt" | "choice";
export type HITLStatus =
  | "pending"
  | "responded"
  | "timeout"
  | "error"
  | "expired";
export type HITLDecision = "allow" | "deny" | "custom" | "timeout";
export type HITLReason =
  | "security"
  | "scope"
  | "cost"
  | "blocker"
  | "nit"
  | "mistake"
  | "other";
export type HITLResumeMode = "continue" | "retry" | "abort" | "alternative";

export const HITL_REASONS: HITLReason[] = [
  "security",
  "scope",
  "cost",
  "blocker",
  "nit",
  "mistake",
  "other",
];

export const HITL_RESUME_MODES: HITLResumeMode[] = [
  "continue",
  "retry",
  "abort",
  "alternative",
];

export interface HITLContext {
  tool_name?: string;
  tool_input_summary?: string;
  target_file?: string;
  command_preview?: string;
}

export interface HITLEvent {
  hitl_id: string;
  span_id: string;
  trace_id: string;
  session_id: string;
  turn_id: string;
  kind: HITLKind | string;
  status: HITLStatus | string;
  requested_at: string;
  responded_at: string | null;
  question: string;
  suggestions: string[];
  context: HITLContext;
  decision: HITLDecision | string | null;
  reason_category: HITLReason | string | null;
  operator_note: string | null;
  resume_mode: HITLResumeMode | string | null;
  operator_id: string | null;
}

export interface HITLResponseInput {
  decision: HITLDecision;
  reason_category?: HITLReason;
  operator_note?: string;
  resume_mode?: HITLResumeMode;
  operator_id?: string;
}

export interface HITLPayload {
  hitl: HITLEvent;
}

export interface HITLListResponse {
  hitl: HITLEvent[];
}

/**
 * Intervention types — mirror internal/store/duckdb/interventions.go and
 * internal/sse/event.go. Operators push text into a live Claude Code session
 * via POST /v1/interventions; the hook at next PreToolUse / UserPromptSubmit
 * claims the row and returns the decision JSON to the agent.
 */
export type InterventionMode = "interrupt" | "context" | "both";
export type InterventionScope = "this_turn" | "this_session";
export type InterventionUrgency = "high" | "normal" | "low";
export type InterventionStatus =
  | "queued"
  | "claimed"
  | "delivered"
  | "consumed"
  | "expired"
  | "cancelled";

export interface Intervention {
  intervention_id: string;
  session_id: string;
  turn_id?: string;
  operator_id?: string;
  created_at: string;
  claimed_at?: string;
  delivered_at?: string;
  consumed_at?: string;
  expired_at?: string;
  cancelled_at?: string;
  auto_expire_at: string;
  message: string;
  delivery_mode: InterventionMode;
  scope: InterventionScope;
  urgency: InterventionUrgency;
  status: InterventionStatus;
  delivered_via?: string;
  consumed_event_id?: number;
  notes?: string;
}

export interface InterventionPayload {
  intervention: Intervention;
}

export interface InterventionListResponse {
  interventions: Intervention[];
}

export interface InterventionCreateRequest {
  session_id: string;
  turn_id?: string;
  operator_id?: string;
  message: string;
  delivery_mode: InterventionMode;
  scope: InterventionScope;
  urgency: InterventionUrgency;
  notes?: string;
  ttl_seconds?: number;
}

/**
 * isHITLEvent narrows the mixed SSE envelope to the HITL sub-union. The
 * wire protocol namespaces HITL payloads under `hitl.*`, so the prefix
 * check is authoritative.
 */
export function isHITLEvent(
  e: AnyApogeeEvent,
): e is ApogeeEvent<HITLPayload> {
  return typeof e.type === "string" && e.type.startsWith("hitl.");
}

/**
 * isInterventionEvent narrows the mixed SSE envelope to the intervention
 * sub-union. The six `intervention.*` lifecycle events all share the same
 * `InterventionPayload` shape.
 */
export function isInterventionEvent(
  e: AnyApogeeEvent,
): e is ApogeeEvent<InterventionPayload> {
  return typeof e.type === "string" && e.type.startsWith("intervention.");
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

/**
 * Agent — per-agent row returned by GET /v1/agents/recent. One row per
 * `(agent_id, agent_kind, agent_type, session_id)` tuple, aggregated over
 * the spans table. Used by the /agents catalog page.
 */
export interface Agent {
  agent_id: string;
  agent_type: string;
  kind: "main" | "subagent" | string;
  parent_agent_id?: string | null;
  session_id: string;
  last_seen: string;
  invocation_count: number;
  total_duration_ms: number;
}

export interface AgentsResponse {
  agents: Agent[];
}

/**
 * AgentDetail mirrors the body of GET /v1/agents/:id/detail (PR #36). Used
 * by the cross-cutting AgentDrawer to render the agent's full identity, the
 * last 20 turns it participated in, a tool histogram, and its parent +
 * direct children.
 */
export interface AgentToolCount {
  name: string;
  count: number;
}

export interface AgentDetailResponse {
  agent: Agent;
  parent: Agent | null;
  children: Agent[];
  turns: Turn[];
  tool_counts: AgentToolCount[];
}

/**
 * SpanDetailResponse mirrors GET /v1/spans/:trace_id/:span_id/detail
 * (PR #36). Used by the cross-cutting SpanDrawer.
 */
export interface SpanDetailResponse {
  span: Span;
  parent: Span | null;
  children: Span[];
}

/**
 * InsightsOverview — aggregate counters returned by
 * GET /v1/insights/overview. All numeric fields are whole-window totals
 * (last 24h for rate-based stats). Populates the /insights page.
 */
export interface ToolCount {
  name: string;
  count: number;
}

export interface PhaseCount {
  name: string;
  count: number;
}

export interface InsightsOverview {
  total_sessions: number;
  total_turns: number;
  total_events: number;
  error_rate_last_24h: number;
  p50_turn_duration_ms: number;
  p95_turn_duration_ms: number;
  top_tools: ToolCount[];
  top_phases: PhaseCount[];
  watchlist_sessions: number;
}

/**
 * ApogeeInfo — collector build + runtime metadata returned by GET /v1/info.
 * Read-only; used by the /settings page.
 */
export interface ApogeeInfo {
  name: string;
  version: string;
  commit: string;
  build_date: string;
  otel_enabled: boolean;
  otel_endpoint: string;
  collector_addr: string;
  uptime_seconds: number;
  // Set when the upgrade-watcher has noticed a newer apogee binary on
  // disk (typical trigger: `brew upgrade apogee`). The dashboard turns
  // these into an UpgradeBanner with a one-click restart.
  update_available?: boolean;
  available_version?: string;
  available_version_detected_at?: string;
}

/**
 * SummarizerLanguage mirrors internal/summarizer.Language* constants. New
 * languages can be added without a wire bump since the field is a union.
 */
export type SummarizerLanguage = "en" | "ja";

/**
 * SummarizerPreferences is the typed view of the summarizer.* keys in the
 * collector's user_preferences table. Every field is optional so PATCH
 * payloads can be sparse — only the keys you set get written.
 */
export interface SummarizerPreferences {
  "summarizer.language"?: SummarizerLanguage;
  "summarizer.recap_system_prompt"?: string;
  "summarizer.rollup_system_prompt"?: string;
  "summarizer.narrative_system_prompt"?: string;
  "summarizer.recap_model"?: string;
  "summarizer.rollup_model"?: string;
  "summarizer.narrative_model"?: string;
}

/** PreferencesResponse mirrors the GET /v1/preferences response body. */
export interface PreferencesResponse {
  preferences: SummarizerPreferences;
  updated_at: Record<string, string>;
}

/**
 * ModelUseCase mirrors internal/summarizer.ModelUseCase. One entry per
 * summarizer tier — the wire shape is a plain string union so new use
 * cases can land without a wire bump.
 */
export type ModelUseCase = "recap" | "rollup" | "narrative";

/**
 * ModelStatus mirrors internal/summarizer.Status* constants. "current"
 * is the live recommendation, "legacy" is a still-usable fallback, and
 * "deprecated" marks catalogue entries on their way out.
 */
export type ModelStatus = "current" | "legacy" | "deprecated";

/**
 * ModelInfo mirrors internal/summarizer.ModelInfo plus the two probe
 * fields (available / checked_at). One entry per static catalog row,
 * served by GET /v1/models.
 */
export interface ModelInfo {
  alias: string;
  short_alias: string;
  family: string;
  generation: string;
  display: string;
  tier: number;
  context_k: number;
  recommended: ModelUseCase[];
  status: ModelStatus;
  available: boolean;
  checked_at: string | null;
}

/** ModelsResponse mirrors the GET /v1/models response body. */
export interface ModelsResponse {
  models: ModelInfo[];
  defaults: {
    recap: string;
    rollup: string;
    narrative: string;
  };
  refreshed_at: string;
}

/**
 * WatchdogSeverity mirrors internal/store/duckdb.WatchdogSeverity*. Tier
 * order is info → warning → critical and is set by the detector based on
 * the absolute z-score it observed.
 */
export type WatchdogSeverity = "info" | "warning" | "critical";

/**
 * WatchdogEvidencePoint is one window sample carried under
 * WatchdogSignal.evidence.window. The detector trims to two decimal
 * places before serialising so the wire payload stays compact.
 */
export interface WatchdogEvidencePoint {
  at: string;
  value: number;
}

/**
 * WatchdogEvidence is the typed payload that rides under
 * WatchdogSignal.evidence. It carries the window samples the detector
 * evaluated against the rolling baseline plus the baseline parameters
 * themselves so the UI can render a sparkline without a second request.
 */
export interface WatchdogEvidence {
  window: WatchdogEvidencePoint[];
  baseline: { mean: number; stddev: number };
  z?: number[];
}

/**
 * WatchdogSignal mirrors internal/sse.WatchdogSnapshot. One row in the
 * watchdog_signals table projected onto its wire shape; nullable
 * columns are unwrapped to nullable fields.
 */
export interface WatchdogSignal {
  id: number;
  detected_at: string;
  ended_at: string | null;
  metric_name: string;
  labels: Record<string, string>;
  z_score: number;
  baseline_mean: number;
  baseline_stddev: number;
  window_value: number;
  severity: WatchdogSeverity;
  headline: string;
  evidence: WatchdogEvidence;
  acknowledged: boolean;
  acknowledged_at: string | null;
}

/** WatchdogPayload is the SSE wrapper for `watchdog.signal` events. */
export interface WatchdogPayload {
  signal: WatchdogSignal;
}

/** WatchdogListResponse mirrors GET /v1/watchdog/signals. */
export interface WatchdogListResponse {
  signals: WatchdogSignal[];
}

/** WatchdogAckResponse mirrors POST /v1/watchdog/signals/:id/ack. */
export interface WatchdogAckResponse {
  signal: WatchdogSignal;
}

/**
 * isWatchdogEvent narrows the mixed SSE envelope to the watchdog
 * sub-union. There is only one watchdog event type (`watchdog.signal`)
 * so the exact-match check is authoritative.
 */
export function isWatchdogEvent(
  e: AnyApogeeEvent,
): e is ApogeeEvent<WatchdogPayload> {
  return e.type === "watchdog.signal";
}

/** TelemetryStatus mirrors the GET /v1/telemetry/status response body. */
export interface TelemetryStatus {
  enabled: boolean;
  endpoint: string;
  protocol: string;
  service_name: string;
  service_version: string;
  service_instance_id: string;
  sample_ratio: number;
  spans_exported_total: number;
}
