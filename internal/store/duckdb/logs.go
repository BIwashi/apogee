package duckdb

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/BIwashi/apogee/internal/otel"
)

// LogRow is the dashboard-facing projection of a stored log record. Field
// order matches what the turn-detail and session-detail raw-log panels render.
type LogRow struct {
	ID             int64          `json:"id"`
	Timestamp      time.Time      `json:"timestamp"`
	TraceID        string         `json:"trace_id,omitempty"`
	SpanID         string         `json:"span_id,omitempty"`
	SeverityText   string         `json:"severity_text"`
	SeverityNumber int            `json:"severity_number"`
	Body           string         `json:"body"`
	SessionID     string          `json:"session_id,omitempty"`
	TurnID        string          `json:"turn_id,omitempty"`
	HookEvent     string          `json:"hook_event"`
	SourceApp     string          `json:"source_app"`
	Attributes    map[string]any  `json:"attributes,omitempty"`
}

// InsertLog appends a log record. The id column is auto-assigned.
func (s *Store) InsertLog(ctx context.Context, l *otel.LogRecord) error {
	const q = `
INSERT INTO logs (
  timestamp, trace_id, span_id, severity_text, severity_number, body,
  session_id, turn_id, hook_event, source_app, attributes_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`
	_, err := s.db.ExecContext(ctx, q,
		l.Timestamp,
		nullString(string(l.TraceID)),
		nullString(string(l.SpanID)),
		defaultString(l.SeverityText, "INFO"),
		defaultIntZero(l.SeverityNumber, 9),
		l.Body,
		nullString(l.SessionID),
		nullString(l.TurnID),
		l.HookEvent,
		l.SourceApp,
		l.AttributesJSON(),
	)
	if err != nil {
		return fmt.Errorf("insert log: %w", err)
	}
	return nil
}

// ListLogsByTurn returns log rows for one turn ordered by timestamp ascending.
// Default limit is 500, max 5000.
func (s *Store) ListLogsByTurn(ctx context.Context, turnID string, limit int) ([]LogRow, error) {
	if limit <= 0 {
		limit = 500
	}
	if limit > 5000 {
		limit = 5000
	}
	const q = `
SELECT id, timestamp, trace_id, span_id, severity_text, severity_number, body,
       session_id, turn_id, hook_event, source_app, attributes_json
FROM logs WHERE turn_id = ? ORDER BY timestamp ASC, id ASC LIMIT ?
`
	rows, err := s.db.QueryContext(ctx, q, turnID, limit)
	if err != nil {
		return nil, fmt.Errorf("list logs by turn: %w", err)
	}
	defer rows.Close()
	return scanLogRows(rows)
}

// LogFilter is the optional set of predicates applied by ListRecentLogs.
// Zero-valued fields are ignored. Before is a cursor: rows with id strictly
// less than Before are returned (newest-first paging).
type LogFilter struct {
	// Before is the exclusive upper bound on `logs.id`. Zero disables the
	// cursor and the most recent rows are returned. The cursor matches the
	// `next_before` returned by the previous page.
	Before int64
	// SessionID restricts the result to a single Claude Code session.
	SessionID string
	// SourceApp restricts the result to a single labelled environment.
	SourceApp string
	// Type restricts the result to a single hook event name (e.g.
	// "PreToolUse").
	Type string
}

// ListRecentLogs returns up to `limit` rows ordered by id DESC (i.e. newest
// first) subject to the provided filter. The second return value is the
// `next_before` cursor — the smallest id in the returned batch, suitable as
// the `Before` value of the next page request. When fewer than `limit` rows
// are returned the caller has reached the end and should stop paginating.
//
// limit defaults to 50 and is clamped to [1, 500].
func (s *Store) ListRecentLogs(ctx context.Context, filter LogFilter, limit int) ([]LogRow, int64, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}

	// Build the WHERE clause dynamically. Args track positional ?'s.
	where := []string{}
	args := []any{}
	if filter.Before > 0 {
		where = append(where, "id < ?")
		args = append(args, filter.Before)
	}
	if filter.SessionID != "" {
		where = append(where, "session_id = ?")
		args = append(args, filter.SessionID)
	}
	if filter.SourceApp != "" {
		where = append(where, "source_app = ?")
		args = append(args, filter.SourceApp)
	}
	if filter.Type != "" {
		where = append(where, "hook_event = ?")
		args = append(args, filter.Type)
	}

	q := `SELECT id, timestamp, trace_id, span_id, severity_text, severity_number, body,
       session_id, turn_id, hook_event, source_app, attributes_json
FROM logs`
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY id DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list recent logs: %w", err)
	}
	defer rows.Close()
	out, err := scanLogRows(rows)
	if err != nil {
		return nil, 0, err
	}
	if len(out) == 0 {
		return out, 0, nil
	}
	// Ordered DESC by id, so the smallest id is the last row in the batch.
	nextCursor := out[len(out)-1].ID
	return out, nextCursor, nil
}

// ListLogsBySession returns log rows for one session ordered by timestamp
// descending so the panel shows the freshest activity first. Default limit
// 200, max 1000.
func (s *Store) ListLogsBySession(ctx context.Context, sessionID string, limit int) ([]LogRow, error) {
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}
	const q = `
SELECT id, timestamp, trace_id, span_id, severity_text, severity_number, body,
       session_id, turn_id, hook_event, source_app, attributes_json
FROM logs WHERE session_id = ? ORDER BY timestamp DESC, id DESC LIMIT ?
`
	rows, err := s.db.QueryContext(ctx, q, sessionID, limit)
	if err != nil {
		return nil, fmt.Errorf("list logs by session: %w", err)
	}
	defer rows.Close()
	return scanLogRows(rows)
}

func scanLogRows(rows *sql.Rows) ([]LogRow, error) {
	var out []LogRow
	for rows.Next() {
		var (
			row            LogRow
			traceID        sql.NullString
			spanID         sql.NullString
			sessionID      sql.NullString
			turnID         sql.NullString
			attributesJSON string
		)
		if err := rows.Scan(
			&row.ID, &row.Timestamp, &traceID, &spanID, &row.SeverityText, &row.SeverityNumber, &row.Body,
			&sessionID, &turnID, &row.HookEvent, &row.SourceApp, &attributesJSON,
		); err != nil {
			return nil, fmt.Errorf("scan log: %w", err)
		}
		if traceID.Valid {
			row.TraceID = traceID.String
		}
		if spanID.Valid {
			row.SpanID = spanID.String
		}
		if sessionID.Valid {
			row.SessionID = sessionID.String
		}
		if turnID.Valid {
			row.TurnID = turnID.String
		}
		row.Attributes = decodeJSONObject(attributesJSON)
		out = append(out, row)
	}
	return out, rows.Err()
}

func defaultIntZero(v, def int) int {
	if v == 0 {
		return def
	}
	return v
}

func decodeJSONObject(s string) map[string]any {
	if s == "" {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil
	}
	return out
}

func decodeJSONArray(s string) []any {
	if s == "" {
		return nil
	}
	var out []any
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil
	}
	return out
}
