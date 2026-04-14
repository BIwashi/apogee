package duckdb

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/BIwashi/apogee/internal/otel"
)

// InsertSpan inserts a freshly opened span. End time and duration may be nil.
func (s *Store) InsertSpan(ctx context.Context, sp *otel.Span) error {
	const q = `
INSERT INTO spans (
  trace_id, span_id, parent_span_id, name, kind, start_time, end_time, duration_ns,
  status_code, status_message, service_name,
  session_id, turn_id, agent_id, agent_kind, tool_name, tool_use_id,
  mcp_server, mcp_tool, hook_event,
  attributes_json, events_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`
	_, err := s.db.ExecContext(ctx, q,
		string(sp.TraceID),
		string(sp.SpanID),
		nullString(string(sp.ParentSpanID)),
		sp.Name,
		string(sp.Kind),
		sp.StartTime,
		nullableTime(sp.EndTime),
		nullableDurationNs(sp),
		string(sp.StatusCode),
		nullString(sp.StatusMessage),
		defaultString(sp.ServiceName, "claude-code"),
		nullString(sp.SessionID),
		nullString(sp.TurnID),
		nullString(sp.AgentID),
		nullString(sp.AgentKind),
		nullString(sp.ToolName),
		nullString(sp.ToolUseID),
		nullString(sp.MCPServer),
		nullString(sp.MCPTool),
		nullString(sp.HookEvent),
		sp.AttributesJSON(),
		sp.EventsJSON(),
	)
	if err != nil {
		return fmt.Errorf("insert span: %w", err)
	}
	return nil
}

// UpdateSpan replaces an existing span row in place. We use this when a span
// transitions from open to closed, or when the attribute bag is updated.
func (s *Store) UpdateSpan(ctx context.Context, sp *otel.Span) error {
	const q = `
UPDATE spans SET
  end_time        = ?,
  duration_ns     = ?,
  status_code     = ?,
  status_message  = ?,
  attributes_json = ?,
  events_json     = ?
WHERE trace_id = ? AND span_id = ?
`
	_, err := s.db.ExecContext(ctx, q,
		nullableTime(sp.EndTime),
		nullableDurationNs(sp),
		string(sp.StatusCode),
		nullString(sp.StatusMessage),
		sp.AttributesJSON(),
		sp.EventsJSON(),
		string(sp.TraceID),
		string(sp.SpanID),
	)
	if err != nil {
		return fmt.Errorf("update span: %w", err)
	}
	return nil
}

// SpanRow is the dashboard-facing projection of a stored span. It is similar
// to otel.Span but uses JSON tags for serialisation.
type SpanRow struct {
	TraceID       string         `json:"trace_id"`
	SpanID        string         `json:"span_id"`
	ParentSpanID  string         `json:"parent_span_id,omitempty"`
	Name          string         `json:"name"`
	Kind          string         `json:"kind"`
	StartTime     time.Time      `json:"start_time"`
	EndTime       *time.Time     `json:"end_time,omitempty"`
	DurationNs    *int64         `json:"duration_ns,omitempty"`
	StatusCode    string         `json:"status_code"`
	StatusMessage string         `json:"status_message,omitempty"`
	ServiceName   string         `json:"service_name"`
	SessionID     string         `json:"session_id,omitempty"`
	TurnID        string         `json:"turn_id,omitempty"`
	AgentID       string         `json:"agent_id,omitempty"`
	AgentKind     string         `json:"agent_kind,omitempty"`
	ToolName      string         `json:"tool_name,omitempty"`
	ToolUseID     string         `json:"tool_use_id,omitempty"`
	MCPServer     string         `json:"mcp_server,omitempty"`
	MCPTool       string         `json:"mcp_tool,omitempty"`
	HookEvent     string         `json:"hook_event,omitempty"`
	Attributes    map[string]any `json:"attributes,omitempty"`
	Events        []any          `json:"events,omitempty"`
}

// GetSpansByTrace returns every span belonging to a trace ordered by start
// time. Useful for the trace-detail view.
func (s *Store) GetSpansByTrace(ctx context.Context, traceID string) ([]SpanRow, error) {
	return s.querySpans(ctx, selectSpan+` WHERE trace_id = ? ORDER BY start_time`, traceID)
}

// GetSpansByTurn returns every span belonging to a turn ordered by start time.
func (s *Store) GetSpansByTurn(ctx context.Context, turnID string) ([]SpanRow, error) {
	return s.querySpans(ctx, selectSpan+` WHERE turn_id = ? ORDER BY start_time`, turnID)
}

// CountToolSpans returns the running total of tool spans. Used by the
// metrics collector to compute per-interval rates.
func (s *Store) CountToolSpans(ctx context.Context) (int64, error) {
	const q = `SELECT COUNT(*) FROM spans WHERE tool_name IS NOT NULL AND tool_name <> ''`
	var n int64
	if err := s.db.QueryRowContext(ctx, q).Scan(&n); err != nil {
		return 0, fmt.Errorf("count tool spans: %w", err)
	}
	return n, nil
}

// CountErrorSpans returns the running total of tool spans that finished with
// status='ERROR'. Used by the metrics collector.
func (s *Store) CountErrorSpans(ctx context.Context) (int64, error) {
	const q = `SELECT COUNT(*) FROM spans WHERE tool_name IS NOT NULL AND tool_name <> '' AND status_code = 'ERROR'`
	var n int64
	if err := s.db.QueryRowContext(ctx, q).Scan(&n); err != nil {
		return 0, fmt.Errorf("count error spans: %w", err)
	}
	return n, nil
}

// CountPendingHITL returns the number of open HITL permission spans.
func (s *Store) CountPendingHITL(ctx context.Context) (int64, error) {
	const q = `SELECT COUNT(*) FROM spans WHERE name = 'claude_code.hitl.permission' AND end_time IS NULL`
	var n int64
	if err := s.db.QueryRowContext(ctx, q).Scan(&n); err != nil {
		return 0, fmt.Errorf("count pending hitl: %w", err)
	}
	return n, nil
}

// ListRecentSpans returns up to limit spans ordered by start_time DESC. This
// is the broad debug feed.
func (s *Store) ListRecentSpans(ctx context.Context, limit int) ([]SpanRow, error) {
	if limit <= 0 {
		limit = 200
	}
	return s.querySpans(ctx, selectSpan+` ORDER BY start_time DESC LIMIT ?`, limit)
}

const selectSpan = `
SELECT
  trace_id, span_id, parent_span_id, name, kind, start_time, end_time, duration_ns,
  status_code, status_message, service_name,
  session_id, turn_id, agent_id, agent_kind, tool_name, tool_use_id,
  mcp_server, mcp_tool, hook_event, attributes_json, events_json
FROM spans
`

func (s *Store) querySpans(ctx context.Context, query string, args ...any) ([]SpanRow, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query spans: %w", err)
	}
	defer rows.Close()
	var out []SpanRow
	for rows.Next() {
		row, err := scanSpanRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *row)
	}
	return out, rows.Err()
}

func scanSpanRow(r rowScanner) (*SpanRow, error) {
	var (
		out             SpanRow
		parentSpanID    sql.NullString
		endTime         sql.NullTime
		durationNs      sql.NullInt64
		statusMessage   sql.NullString
		sessionID       sql.NullString
		turnID          sql.NullString
		agentID         sql.NullString
		agentKind       sql.NullString
		toolName        sql.NullString
		toolUseID       sql.NullString
		mcpServer       sql.NullString
		mcpTool         sql.NullString
		hookEvent       sql.NullString
		attributesJSON  string
		eventsJSON      string
	)
	if err := r.Scan(
		&out.TraceID, &out.SpanID, &parentSpanID, &out.Name, &out.Kind, &out.StartTime, &endTime, &durationNs,
		&out.StatusCode, &statusMessage, &out.ServiceName,
		&sessionID, &turnID, &agentID, &agentKind, &toolName, &toolUseID,
		&mcpServer, &mcpTool, &hookEvent, &attributesJSON, &eventsJSON,
	); err != nil {
		return nil, err
	}
	if parentSpanID.Valid {
		out.ParentSpanID = parentSpanID.String
	}
	if endTime.Valid {
		v := endTime.Time
		out.EndTime = &v
	}
	if durationNs.Valid {
		v := durationNs.Int64
		out.DurationNs = &v
	}
	if statusMessage.Valid {
		out.StatusMessage = statusMessage.String
	}
	if sessionID.Valid {
		out.SessionID = sessionID.String
	}
	if turnID.Valid {
		out.TurnID = turnID.String
	}
	if agentID.Valid {
		out.AgentID = agentID.String
	}
	if agentKind.Valid {
		out.AgentKind = agentKind.String
	}
	if toolName.Valid {
		out.ToolName = toolName.String
	}
	if toolUseID.Valid {
		out.ToolUseID = toolUseID.String
	}
	if mcpServer.Valid {
		out.MCPServer = mcpServer.String
	}
	if mcpTool.Valid {
		out.MCPTool = mcpTool.String
	}
	if hookEvent.Valid {
		out.HookEvent = hookEvent.String
	}
	out.Attributes = decodeJSONObject(attributesJSON)
	out.Events = decodeJSONArray(eventsJSON)
	return &out, nil
}

func nullableDurationNs(sp *otel.Span) any {
	if sp.EndTime == nil {
		return nil
	}
	return sp.DurationNanos()
}

func defaultString(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
