-- Sessions: one row per Claude Code session_id seen.
CREATE SEQUENCE IF NOT EXISTS sessions_row_id_seq;
CREATE TABLE IF NOT EXISTS sessions (
  session_id      VARCHAR PRIMARY KEY,
  source_app      VARCHAR NOT NULL,
  started_at      TIMESTAMP NOT NULL,
  ended_at        TIMESTAMP,
  last_seen_at    TIMESTAMP NOT NULL,
  turn_count      INTEGER NOT NULL DEFAULT 0,
  model           VARCHAR,
  machine_id      VARCHAR
);
CREATE INDEX IF NOT EXISTS idx_sessions_last_seen ON sessions(last_seen_at DESC);
CREATE INDEX IF NOT EXISTS idx_sessions_source_app ON sessions(source_app);

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
  attention_score    DOUBLE
);
CREATE INDEX IF NOT EXISTS idx_turns_session ON turns(session_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_turns_recent ON turns(started_at DESC);
CREATE INDEX IF NOT EXISTS idx_turns_status ON turns(status);
CREATE INDEX IF NOT EXISTS idx_turns_attention ON turns(attention_state);

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
