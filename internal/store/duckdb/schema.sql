-- Sessions: one row per Claude Code session_id seen. The *_live columns
-- below cache the fleet-view health of a session so the Live dashboard can
-- render one row per terminal without scanning turns on every poll. They
-- are written back by the attention engine via UpdateSessionAttention, and
-- by the summarizer's live_status worker (live_status_text). All default
-- to NULL so pre-engine / pre-summarizer rows degrade gracefully.
CREATE SEQUENCE IF NOT EXISTS sessions_row_id_seq;
CREATE TABLE IF NOT EXISTS sessions (
  session_id        VARCHAR PRIMARY KEY,
  source_app        VARCHAR NOT NULL,
  started_at        TIMESTAMP NOT NULL,
  ended_at          TIMESTAMP,
  last_seen_at      TIMESTAMP NOT NULL,
  turn_count        INTEGER NOT NULL DEFAULT 0,
  model             VARCHAR,
  machine_id        VARCHAR,
  attention_state   VARCHAR,
  attention_reason  VARCHAR,
  attention_score   DOUBLE,
  current_turn_id   VARCHAR,
  current_phase     VARCHAR,
  live_state        VARCHAR,
  live_status_text  VARCHAR,
  live_status_at    TIMESTAMP,
  live_status_model VARCHAR
);
CREATE INDEX IF NOT EXISTS idx_sessions_last_seen ON sessions(last_seen_at DESC);
CREATE INDEX IF NOT EXISTS idx_sessions_source_app ON sessions(source_app);
-- idx_sessions_attention is created in applyColumnAdditions (migrate.go)
-- because attention_state is a late-added column that may not exist when
-- schema.sql runs against an older database for the first time.

-- Turns: one row per user turn (one trace).
CREATE TABLE IF NOT EXISTS turns (
  turn_id            VARCHAR PRIMARY KEY,
  trace_id           VARCHAR NOT NULL UNIQUE,
  session_id         VARCHAR NOT NULL,
  source_app         VARCHAR NOT NULL,
  started_at         TIMESTAMP NOT NULL,
  ended_at           TIMESTAMP,
  duration_ms        BIGINT,
  status             VARCHAR NOT NULL,
  model              VARCHAR,
  prompt_text        VARCHAR,
  prompt_chars       INTEGER,
  output_chars       INTEGER,
  tool_call_count    INTEGER NOT NULL DEFAULT 0,
  subagent_count     INTEGER NOT NULL DEFAULT 0,
  error_count        INTEGER NOT NULL DEFAULT 0,
  input_tokens       BIGINT,
  output_tokens      BIGINT,
  headline           VARCHAR,
  outcome_summary    VARCHAR,
  attention_state    VARCHAR,
  attention_reason   VARCHAR,
  attention_score    DOUBLE,
  attention_tone     VARCHAR,
  phase              VARCHAR,
  phase_confidence   DOUBLE,
  phase_since        TIMESTAMP,
  attention_signals_json VARCHAR,
  recap_json             VARCHAR,
  recap_generated_at     TIMESTAMP,
  recap_model            VARCHAR
);
CREATE INDEX IF NOT EXISTS idx_turns_session ON turns(session_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_turns_recent ON turns(started_at DESC);
CREATE INDEX IF NOT EXISTS idx_turns_status ON turns(status);
CREATE INDEX IF NOT EXISTS idx_turns_attention ON turns(attention_state);

-- Task type history: rolling success/failure counts keyed by the canonical
-- tool signature of a turn. Populated by the attention engine when a turn
-- closes, and read back for the watchlist bucket.
CREATE TABLE IF NOT EXISTS task_type_history (
  pattern        VARCHAR PRIMARY KEY,
  turn_count     BIGINT NOT NULL DEFAULT 0,
  success_count  BIGINT NOT NULL DEFAULT 0,
  failure_count  BIGINT NOT NULL DEFAULT 0,
  last_updated   TIMESTAMP NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_task_type_history_updated ON task_type_history(last_updated DESC);

-- Spans: OTel trace data.
CREATE TABLE IF NOT EXISTS spans (
  trace_id          VARCHAR NOT NULL,
  span_id           VARCHAR NOT NULL,
  parent_span_id    VARCHAR,
  name              VARCHAR NOT NULL,
  kind              VARCHAR NOT NULL DEFAULT 'INTERNAL',
  start_time        TIMESTAMP NOT NULL,
  end_time          TIMESTAMP,
  duration_ns       BIGINT,
  status_code       VARCHAR NOT NULL DEFAULT 'UNSET',
  status_message    VARCHAR,
  service_name      VARCHAR NOT NULL DEFAULT 'claude-code',
  session_id        VARCHAR,
  turn_id           VARCHAR,
  agent_id          VARCHAR,
  agent_kind        VARCHAR,
  tool_name         VARCHAR,
  tool_use_id       VARCHAR,
  mcp_server        VARCHAR,
  mcp_tool          VARCHAR,
  hook_event        VARCHAR,
  attributes_json   VARCHAR NOT NULL DEFAULT '{}',
  events_json       VARCHAR NOT NULL DEFAULT '[]',
  PRIMARY KEY (trace_id, span_id)
);
CREATE INDEX IF NOT EXISTS idx_spans_trace ON spans(trace_id);
CREATE INDEX IF NOT EXISTS idx_spans_session ON spans(session_id, start_time);
CREATE INDEX IF NOT EXISTS idx_spans_turn ON spans(turn_id, start_time);
CREATE INDEX IF NOT EXISTS idx_spans_tool ON spans(tool_name);
CREATE INDEX IF NOT EXISTS idx_spans_name ON spans(name);
CREATE INDEX IF NOT EXISTS idx_spans_start ON spans(start_time DESC);

-- Logs: OTel log records. One per hook event so the raw log view is lossless.
CREATE SEQUENCE IF NOT EXISTS logs_id_seq;
CREATE TABLE IF NOT EXISTS logs (
  id              BIGINT PRIMARY KEY DEFAULT nextval('logs_id_seq'),
  timestamp       TIMESTAMP NOT NULL,
  trace_id        VARCHAR,
  span_id         VARCHAR,
  severity_text   VARCHAR NOT NULL DEFAULT 'INFO',
  severity_number INTEGER NOT NULL DEFAULT 9,
  body            VARCHAR NOT NULL,
  session_id      VARCHAR,
  turn_id         VARCHAR,
  hook_event      VARCHAR NOT NULL,
  source_app      VARCHAR NOT NULL,
  attributes_json VARCHAR NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS idx_logs_session ON logs(session_id, timestamp);
CREATE INDEX IF NOT EXISTS idx_logs_trace ON logs(trace_id);
CREATE INDEX IF NOT EXISTS idx_logs_hook ON logs(hook_event);
CREATE INDEX IF NOT EXISTS idx_logs_timestamp ON logs(timestamp DESC);

-- HITL events: structured Human-In-The-Loop request/response records. One
-- row per permission/tool_approval/prompt/choice request. The lifecycle
-- moves status from pending -> responded|timeout|expired|error.
CREATE SEQUENCE IF NOT EXISTS hitl_events_id_seq;
CREATE TABLE IF NOT EXISTS hitl_events (
  id                BIGINT PRIMARY KEY DEFAULT nextval('hitl_events_id_seq'),
  hitl_id           VARCHAR NOT NULL UNIQUE,
  span_id           VARCHAR NOT NULL,
  trace_id          VARCHAR NOT NULL,
  session_id        VARCHAR NOT NULL,
  turn_id           VARCHAR NOT NULL,
  kind              VARCHAR NOT NULL,
  status            VARCHAR NOT NULL,
  requested_at      TIMESTAMP NOT NULL,
  responded_at      TIMESTAMP,
  question          VARCHAR NOT NULL,
  suggestions_json  VARCHAR NOT NULL DEFAULT '[]',
  context_json      VARCHAR NOT NULL DEFAULT '{}',
  decision          VARCHAR,
  reason_category   VARCHAR,
  operator_note     VARCHAR,
  resume_mode       VARCHAR,
  operator_id       VARCHAR
);
CREATE INDEX IF NOT EXISTS idx_hitl_session_time ON hitl_events(session_id, requested_at DESC);
CREATE INDEX IF NOT EXISTS idx_hitl_turn ON hitl_events(turn_id);
CREATE INDEX IF NOT EXISTS idx_hitl_status ON hitl_events(status);
CREATE INDEX IF NOT EXISTS idx_hitl_kind ON hitl_events(kind);

-- Session rollups: long-range narrative digests produced by the summarizer's
-- second tier (Sonnet). One row per session — replaced in place by the
-- UpsertSessionRollup path when a new rollup lands.
CREATE TABLE IF NOT EXISTS session_rollups (
  session_id      VARCHAR PRIMARY KEY,
  generated_at    TIMESTAMP NOT NULL,
  model           VARCHAR NOT NULL,
  from_turn_id    VARCHAR,
  to_turn_id      VARCHAR,
  turn_count      INTEGER NOT NULL,
  rollup_json     VARCHAR NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_rollups_generated ON session_rollups(generated_at DESC);

-- Agent summaries: per-(agent, session) LLM-generated label produced by the
-- summarizer's agent worker (Haiku tier). Surfaced in the /agents catalog
-- so each row carries a "what is this agent doing?" headline instead of the
-- generic agent_type label. One row per (agent_id, session_id), replaced
-- in place by the worker when activity grows past the staleness threshold.
CREATE TABLE IF NOT EXISTS agent_summaries (
  agent_id        VARCHAR NOT NULL,
  session_id      VARCHAR NOT NULL,
  generated_at    TIMESTAMP NOT NULL,
  model           VARCHAR NOT NULL,
  title           VARCHAR NOT NULL,
  role            VARCHAR,
  summary_json    VARCHAR NOT NULL,
  invocation_count_at_generation BIGINT NOT NULL,
  PRIMARY KEY (agent_id, session_id)
);
CREATE INDEX IF NOT EXISTS idx_agent_summaries_session ON agent_summaries(session_id);
CREATE INDEX IF NOT EXISTS idx_agent_summaries_generated ON agent_summaries(generated_at DESC);

-- Interventions: operator-initiated messages pushed into a live Claude Code
-- session. The lifecycle is queued -> claimed -> delivered -> consumed,
-- with cancelled / expired as terminal off-ramps. A claim is an atomic
-- flip executed by the hook at PreToolUse/UserPromptSubmit time.
CREATE TABLE IF NOT EXISTS interventions (
  intervention_id    VARCHAR PRIMARY KEY,
  session_id         VARCHAR NOT NULL,
  turn_id            VARCHAR,
  operator_id        VARCHAR,
  created_at         TIMESTAMP NOT NULL,
  claimed_at         TIMESTAMP,
  delivered_at       TIMESTAMP,
  consumed_at        TIMESTAMP,
  expired_at         TIMESTAMP,
  cancelled_at       TIMESTAMP,
  auto_expire_at     TIMESTAMP NOT NULL,
  message            VARCHAR NOT NULL,
  delivery_mode      VARCHAR NOT NULL,
  scope              VARCHAR NOT NULL,
  urgency            VARCHAR NOT NULL,
  status             VARCHAR NOT NULL,
  delivered_via      VARCHAR,
  consumed_event_id  BIGINT,
  notes              VARCHAR
);
CREATE INDEX IF NOT EXISTS idx_interventions_session_created ON interventions(session_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_interventions_pending ON interventions(session_id, status);
CREATE INDEX IF NOT EXISTS idx_interventions_status ON interventions(status);
CREATE INDEX IF NOT EXISTS idx_interventions_auto_expire ON interventions(auto_expire_at);

-- Metric points: OTel metric data. Write-optimized columnar.
CREATE SEQUENCE IF NOT EXISTS metric_points_id_seq;
CREATE TABLE IF NOT EXISTS metric_points (
  id             BIGINT PRIMARY KEY DEFAULT nextval('metric_points_id_seq'),
  timestamp      TIMESTAMP NOT NULL,
  name           VARCHAR NOT NULL,
  kind           VARCHAR NOT NULL,
  value          DOUBLE,
  histogram_json VARCHAR,
  unit           VARCHAR,
  labels_json    VARCHAR NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS idx_metric_points_name ON metric_points(name, timestamp DESC);

-- User preferences: a generic K/V table of UI / runtime knobs the operator
-- tweaks from the dashboard. Values are JSON-encoded so future preferences
-- can be richer than scalar strings (lists, objects). Owned by PR #29 — see
-- internal/store/duckdb/preferences.go for the typed accessors and
-- internal/collector/preferences.go for the HTTP routes.
CREATE TABLE IF NOT EXISTS user_preferences (
  key           VARCHAR PRIMARY KEY,
  value_json    VARCHAR NOT NULL,
  updated_at    TIMESTAMP NOT NULL
);

-- Watchdog signals: anomaly detections emitted by the background worker.
-- One row per "spell" — a period where a metric_points tuple deviates from
-- its rolling 24h baseline by more than 3 standard deviations. The
-- detector dedupes while the metric stays anomalous so the UI does not
-- see alert storms. See internal/watchdog/ for the math and
-- internal/store/duckdb/watchdog_signals.go for the CRUD.
CREATE SEQUENCE IF NOT EXISTS watchdog_signals_id_seq;
CREATE TABLE IF NOT EXISTS watchdog_signals (
  id               BIGINT PRIMARY KEY DEFAULT nextval('watchdog_signals_id_seq'),
  detected_at      TIMESTAMP NOT NULL,
  ended_at         TIMESTAMP,
  metric_name      VARCHAR NOT NULL,
  labels_json      VARCHAR NOT NULL DEFAULT '{}',
  z_score          DOUBLE NOT NULL,
  baseline_mean    DOUBLE NOT NULL,
  baseline_stddev  DOUBLE NOT NULL,
  window_value     DOUBLE NOT NULL,
  severity         VARCHAR NOT NULL,
  headline         VARCHAR NOT NULL,
  evidence_json    VARCHAR NOT NULL DEFAULT '{}',
  acknowledged     BOOLEAN NOT NULL DEFAULT FALSE,
  acknowledged_at  TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_watchdog_detected ON watchdog_signals(detected_at DESC);
CREATE INDEX IF NOT EXISTS idx_watchdog_unacked ON watchdog_signals(acknowledged);

-- Model availability cache: one row per Claude model alias apogee knows
-- about, recording whether the local `claude` CLI accepted the alias on
-- the most recent probe. Populated by summarizer.Probe (see
-- internal/summarizer/models_probe.go) and consumed by the /v1/models
-- HTTP route and the summarizer workers' resolver. Rows expire via
-- PruneStaleAvailability (24h default TTL) so stale probes don't
-- permanently hide a model that came back online.
CREATE TABLE IF NOT EXISTS model_availability (
  alias      VARCHAR PRIMARY KEY,
  available  BOOLEAN NOT NULL,
  checked_at TIMESTAMP NOT NULL,
  last_error VARCHAR
);
