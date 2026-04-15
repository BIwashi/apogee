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

// LogFilter is the optional set of predicates applied by ListRecentLogs,
// EventFacets, and EventTimeseries. Zero-valued fields are ignored. Before is
// a cursor: rows with id strictly less than Before are returned
// (newest-first paging).
//
// PR #37 extends the filter to support the Datadog-style left-rail facet
// panel: multi-select SourceApps/HookEvents/Severities/Sessions, a free-text
// Query, and an explicit Since/Until time window. The singular SourceApp /
// Type / SessionID fields are kept for backward compatibility with the
// existing /v1/events/recent callers — they are folded into their plural
// counterparts at filter-build time.
type LogFilter struct {
	// Before is the exclusive upper bound on `logs.id`. Zero disables the
	// cursor and the most recent rows are returned. The cursor matches the
	// `next_before` returned by the previous page.
	Before int64
	// SessionID restricts the result to a single Claude Code session.
	// Deprecated: use Sessions. Singular form is preserved for
	// backward compatibility.
	SessionID string
	// SourceApp restricts the result to a single labelled environment.
	// Deprecated: use SourceApps.
	SourceApp string
	// Type restricts the result to a single hook event name (e.g.
	// "PreToolUse"). Deprecated: use HookEvents.
	Type string

	// Since is the inclusive lower bound on `logs.timestamp`. Zero means
	// "no lower bound".
	Since time.Time
	// Until is the inclusive upper bound on `logs.timestamp`. Zero means
	// "no upper bound".
	Until time.Time
	// SourceApps is the multi-select equivalent of SourceApp.
	SourceApps []string
	// HookEvents is the multi-select equivalent of Type.
	HookEvents []string
	// Severities is a multi-select list of severity_text values
	// (e.g. "INFO", "ERROR"). Matching is case-insensitive via DuckDB's
	// UPPER().
	Severities []string
	// Sessions is the multi-select equivalent of SessionID.
	Sessions []string
	// Query, when non-empty, is applied as `body LIKE '%Query%'`.
	// Case-sensitive; callers normalise the input.
	Query string
}

// buildWhere assembles a WHERE clause + positional args for the given
// filter. The returned args slice is appended to by callers so both
// ListRecentLogs and the facet/timeseries helpers share a single
// canonicalisation pass for the filter.
//
// The caller is responsible for pasting the clause into the SQL template;
// if the filter is empty the returned clause is the empty string and it
// should not be appended.
func (f *LogFilter) buildWhere() (string, []any) {
	where := []string{}
	args := []any{}

	// Fold singular into plural for backward compat.
	sourceApps := append([]string(nil), f.SourceApps...)
	if f.SourceApp != "" {
		sourceApps = append(sourceApps, f.SourceApp)
	}
	hookEvents := append([]string(nil), f.HookEvents...)
	if f.Type != "" {
		hookEvents = append(hookEvents, f.Type)
	}
	sessions := append([]string(nil), f.Sessions...)
	if f.SessionID != "" {
		sessions = append(sessions, f.SessionID)
	}

	if f.Before > 0 {
		where = append(where, "id < ?")
		args = append(args, f.Before)
	}
	if !f.Since.IsZero() {
		where = append(where, "timestamp >= ?")
		args = append(args, f.Since)
	}
	if !f.Until.IsZero() {
		where = append(where, "timestamp <= ?")
		args = append(args, f.Until)
	}
	if len(sessions) > 0 {
		where = append(where, "session_id IN ("+placeholders(len(sessions))+")")
		for _, v := range sessions {
			args = append(args, v)
		}
	}
	if len(sourceApps) > 0 {
		where = append(where, "source_app IN ("+placeholders(len(sourceApps))+")")
		for _, v := range sourceApps {
			args = append(args, v)
		}
	}
	if len(hookEvents) > 0 {
		where = append(where, "hook_event IN ("+placeholders(len(hookEvents))+")")
		for _, v := range hookEvents {
			args = append(args, v)
		}
	}
	if len(f.Severities) > 0 {
		norm := make([]string, 0, len(f.Severities))
		for _, v := range f.Severities {
			norm = append(norm, strings.ToUpper(v))
		}
		where = append(where, "UPPER(severity_text) IN ("+placeholders(len(norm))+")")
		for _, v := range norm {
			args = append(args, v)
		}
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		where = append(where, "body LIKE ?")
		args = append(args, "%"+q+"%")
	}

	if len(where) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(where, " AND "), args
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	out := make([]byte, 0, n*2)
	for i := 0; i < n; i++ {
		if i > 0 {
			out = append(out, ',')
		}
		out = append(out, '?')
	}
	return string(out)
}

// FacetValue is one entry inside a FacetDimension: a distinct value of the
// facet key plus the count of rows in the current filter that carry it.
type FacetValue struct {
	Value string `json:"value"`
	Count int64  `json:"count"`
}

// FacetDimension is a single facet column on the /events page left rail —
// e.g. "source_app" with a list of top values. Values are ordered
// descending by count and capped at 50 entries so the UI stays responsive
// on datasets with high cardinality session_id columns.
type FacetDimension struct {
	Key    string       `json:"key"`
	Values []FacetValue `json:"values"`
}

// facetKeys is the fixed list of dimensions the dashboard surfaces. Keep
// in lockstep with web/app/components/FacetPanel.tsx.
var facetKeys = []struct {
	Key    string
	Column string
}{
	{"source_app", "source_app"},
	{"hook_event", "hook_event"},
	{"severity_text", "severity_text"},
	{"session_id", "session_id"},
}

// EventFacets returns the top 50 distinct values + counts for each of the
// 4 fixed facet dimensions, honouring the supplied filter. Each dimension
// is computed with its own GROUP BY query; on 1M rows DuckDB completes the
// whole batch in single-digit ms because logs is fully columnar.
//
// The returned slice order matches facetKeys so clients can render the
// groups in a stable sequence. Dimensions with no rows still appear with
// an empty Values list so the left rail shows every group header.
func (s *Store) EventFacets(ctx context.Context, filter LogFilter) ([]FacetDimension, error) {
	whereClause, whereArgs := filter.buildWhere()
	out := make([]FacetDimension, 0, len(facetKeys))
	for _, fk := range facetKeys {
		// Exclude NULL / empty values — the dashboard has nothing
		// meaningful to show for a facet bucket keyed on an empty
		// string, and including them would crowd out the real top N.
		q := fmt.Sprintf(
			"SELECT %[1]s AS v, COUNT(*) AS c FROM logs%[2]s%[3]s %[1]s IS NOT NULL AND %[1]s <> '' GROUP BY 1 ORDER BY c DESC, 1 ASC LIMIT 50",
			fk.Column,
			whereClause,
			whereClauseJoiner(whereClause),
		)
		rows, err := s.db.QueryContext(ctx, q, whereArgs...)
		if err != nil {
			return nil, fmt.Errorf("event facets %s: %w", fk.Key, err)
		}
		dim := FacetDimension{Key: fk.Key, Values: []FacetValue{}}
		for rows.Next() {
			var val sql.NullString
			var count int64
			if err := rows.Scan(&val, &count); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan facet %s: %w", fk.Key, err)
			}
			if !val.Valid || val.String == "" {
				continue
			}
			dim.Values = append(dim.Values, FacetValue{Value: val.String, Count: count})
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		out = append(out, dim)
	}
	return out, nil
}

// whereClauseJoiner returns " AND" when the clause is non-empty so the
// EventFacets template can tack on the "col IS NOT NULL" predicate without
// duplicating the WHERE keyword.
func whereClauseJoiner(whereClause string) string {
	if whereClause == "" {
		return " WHERE"
	}
	return " AND"
}

// EventBucket is one time-series bucket returned by EventTimeseries. Total
// is the sum of all logs in the bucket; BySeverity is a pre-aggregated
// breakdown keyed by the lowercased severity text ("info", "warn", "error",
// "debug", ...). Severities with zero entries in the bucket are omitted.
type EventBucket struct {
	Bucket     time.Time        `json:"bucket"`
	Total      int64            `json:"total"`
	BySeverity map[string]int64 `json:"by_severity"`
}

// EventTimeseries returns evenly-spaced buckets over the window described
// by filter.Since / filter.Until (defaulting to the last hour when both
// are zero). Each bucket carries the total count plus a breakdown by
// severity text — the web side uses the severity split to colour-code the
// stacked-bar histogram on /events.
//
// step is the bucket width. Callers typically scale step to the window
// length so the result fits comfortably in a few hundred bars:
//
//	1 min  → 1 s
//	1 h    → 30 s
//	24 h   → 10 min
//	7 d    → 1 h
//
// If step is zero the default is 30 s. Buckets with no data in the
// underlying table do not appear in the result; the web side pads zero
// buckets when it draws the chart so missing ranges stay visible.
func (s *Store) EventTimeseries(ctx context.Context, filter LogFilter, step time.Duration) ([]EventBucket, error) {
	if step <= 0 {
		step = 30 * time.Second
	}
	// If the caller forgot to set a time window, default to "last hour"
	// against the wall clock so the endpoint never returns the entire
	// table — that would happily OOM the dashboard.
	if filter.Since.IsZero() && filter.Until.IsZero() {
		now := time.Now().UTC()
		filter.Since = now.Add(-time.Hour)
		filter.Until = now
	}

	whereClause, whereArgs := filter.buildWhere()
	stepSeconds := int64(step / time.Second)
	if stepSeconds <= 0 {
		stepSeconds = 1
	}

	q := fmt.Sprintf(`
SELECT time_bucket(INTERVAL (? || ' seconds'), timestamp) AS bucket,
       LOWER(severity_text) AS sev,
       COUNT(*) AS c
FROM logs%s
GROUP BY 1, 2
ORDER BY 1 ASC`, whereClause)

	args := make([]any, 0, len(whereArgs)+1)
	args = append(args, stepSeconds)
	args = append(args, whereArgs...)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("event timeseries: %w", err)
	}
	defer rows.Close()

	bucketMap := make(map[int64]*EventBucket)
	order := []int64{}
	for rows.Next() {
		var bucket time.Time
		var sev sql.NullString
		var count int64
		if err := rows.Scan(&bucket, &sev, &count); err != nil {
			return nil, fmt.Errorf("scan timeseries bucket: %w", err)
		}
		key := bucket.Unix()
		b, ok := bucketMap[key]
		if !ok {
			b = &EventBucket{
				Bucket:     bucket,
				BySeverity: map[string]int64{},
			}
			bucketMap[key] = b
			order = append(order, key)
		}
		b.Total += count
		sevKey := "info"
		if sev.Valid && sev.String != "" {
			sevKey = sev.String
		}
		b.BySeverity[sevKey] += count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]EventBucket, 0, len(order))
	for _, k := range order {
		out = append(out, *bucketMap[k])
	}
	return out, nil
}

// CountEvents returns the total number of logs matching filter. Used by
// the dashboard to render the "12,450 events found" header above the
// histogram without a second round-trip.
func (s *Store) CountEvents(ctx context.Context, filter LogFilter) (int64, error) {
	whereClause, whereArgs := filter.buildWhere()
	q := "SELECT COUNT(*) FROM logs" + whereClause
	var n int64
	if err := s.db.QueryRowContext(ctx, q, whereArgs...).Scan(&n); err != nil {
		return 0, fmt.Errorf("count events: %w", err)
	}
	return n, nil
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

	whereClause, args := filter.buildWhere()
	q := `SELECT id, timestamp, trace_id, span_id, severity_text, severity_number, body,
       session_id, turn_id, hook_event, source_app, attributes_json
FROM logs` + whereClause + " ORDER BY id DESC LIMIT ?"
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
