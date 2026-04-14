package duckdb

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/BIwashi/apogee/internal/otel"
)

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
