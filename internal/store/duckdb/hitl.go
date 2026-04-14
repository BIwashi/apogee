package duckdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// HITLEvent is a row in the hitl_events table. Free-form fields like the
// question and operator note are kept as plain strings; structured payloads
// (suggestions, context) are stored as JSON blobs and decoded by the API
// layer when serving HTTP responses.
type HITLEvent struct {
	ID              int64          `json:"id"`
	HitlID          string         `json:"hitl_id"`
	SpanID          string         `json:"span_id"`
	TraceID         string         `json:"trace_id"`
	SessionID       string         `json:"session_id"`
	TurnID          string         `json:"turn_id"`
	Kind            string         `json:"kind"`
	Status          string         `json:"status"`
	RequestedAt     time.Time      `json:"requested_at"`
	RespondedAt     sql.NullTime   `json:"-"`
	Question        string         `json:"question"`
	SuggestionsJSON string         `json:"-"`
	ContextJSON     string         `json:"-"`
	Decision        sql.NullString `json:"-"`
	ReasonCategory  sql.NullString `json:"-"`
	OperatorNote    sql.NullString `json:"-"`
	ResumeMode      sql.NullString `json:"-"`
	OperatorID      sql.NullString `json:"-"`
}

// HITLResponse carries the operator's structured reply to a pending HITL
// request. All fields except Decision are optional.
type HITLResponse struct {
	Decision       string `json:"decision"`
	ReasonCategory string `json:"reason_category,omitempty"`
	OperatorNote   string `json:"operator_note,omitempty"`
	ResumeMode     string `json:"resume_mode,omitempty"`
	OperatorID     string `json:"operator_id,omitempty"`
}

// HITLFilter narrows ListRecentHITL queries.
type HITLFilter struct {
	SessionID string
	Status    string
	Kind      string
}

// HITL lifecycle status constants.
const (
	HITLStatusPending   = "pending"
	HITLStatusResponded = "responded"
	HITLStatusTimeout   = "timeout"
	HITLStatusError     = "error"
	HITLStatusExpired   = "expired"
)

// ErrHITLNotFound is returned by RespondHITL/GetHITL when no row matches.
var ErrHITLNotFound = errors.New("hitl event not found")

// ErrHITLAlreadyResponded is returned by RespondHITL when the row is no
// longer in the pending state.
var ErrHITLAlreadyResponded = errors.New("hitl event already finalised")

const selectHITL = `
SELECT
  id, hitl_id, span_id, trace_id, session_id, turn_id, kind, status,
  requested_at, responded_at, question, suggestions_json, context_json,
  decision, reason_category, operator_note, resume_mode, operator_id
FROM hitl_events
`

// InsertHITL writes a fresh hitl_events row. The caller is responsible for
// generating a unique HitlID and supplying RequestedAt/Status/Question.
func (s *Store) InsertHITL(ctx context.Context, ev HITLEvent) error {
	if ev.SuggestionsJSON == "" {
		ev.SuggestionsJSON = "[]"
	}
	if ev.ContextJSON == "" {
		ev.ContextJSON = "{}"
	}
	if ev.Status == "" {
		ev.Status = HITLStatusPending
	}
	const q = `
INSERT INTO hitl_events (
  hitl_id, span_id, trace_id, session_id, turn_id, kind, status,
  requested_at, responded_at, question, suggestions_json, context_json,
  decision, reason_category, operator_note, resume_mode, operator_id
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`
	_, err := s.db.ExecContext(ctx, q,
		ev.HitlID,
		ev.SpanID,
		ev.TraceID,
		ev.SessionID,
		ev.TurnID,
		ev.Kind,
		ev.Status,
		ev.RequestedAt,
		nullableSQLTime(ev.RespondedAt),
		ev.Question,
		ev.SuggestionsJSON,
		ev.ContextJSON,
		nullableSQLString(ev.Decision),
		nullableSQLString(ev.ReasonCategory),
		nullableSQLString(ev.OperatorNote),
		nullableSQLString(ev.ResumeMode),
		nullableSQLString(ev.OperatorID),
	)
	if err != nil {
		return fmt.Errorf("insert hitl: %w", err)
	}
	return nil
}

// GetHITL fetches one row by hitl_id. The bool return is false when the row
// does not exist (and err is nil).
func (s *Store) GetHITL(ctx context.Context, hitlID string) (HITLEvent, bool, error) {
	row := s.db.QueryRowContext(ctx, selectHITL+` WHERE hitl_id = ?`, hitlID)
	ev, err := scanHITL(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return HITLEvent{}, false, nil
		}
		return HITLEvent{}, false, fmt.Errorf("get hitl: %w", err)
	}
	return ev, true, nil
}

// RespondHITL atomically transitions a pending row to responded and records
// the operator's reply. Returns ErrHITLNotFound or ErrHITLAlreadyResponded
// when the precondition fails.
func (s *Store) RespondHITL(ctx context.Context, hitlID string, resp HITLResponse, now time.Time) (HITLEvent, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return HITLEvent{}, fmt.Errorf("respond hitl: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var status string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM hitl_events WHERE hitl_id = ?`, hitlID).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return HITLEvent{}, ErrHITLNotFound
		}
		return HITLEvent{}, fmt.Errorf("respond hitl: lookup: %w", err)
	}
	if status != HITLStatusPending {
		return HITLEvent{}, ErrHITLAlreadyResponded
	}

	const upd = `
UPDATE hitl_events SET
  status          = ?,
  responded_at    = ?,
  decision        = ?,
  reason_category = ?,
  operator_note   = ?,
  resume_mode     = ?,
  operator_id     = ?
WHERE hitl_id = ?
`
	if _, err := tx.ExecContext(ctx, upd,
		HITLStatusResponded,
		now,
		nullString(resp.Decision),
		nullString(resp.ReasonCategory),
		nullString(resp.OperatorNote),
		nullString(resp.ResumeMode),
		nullString(resp.OperatorID),
		hitlID,
	); err != nil {
		return HITLEvent{}, fmt.Errorf("respond hitl: update: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return HITLEvent{}, fmt.Errorf("respond hitl: commit: %w", err)
	}

	out, ok, err := s.GetHITL(ctx, hitlID)
	if err != nil {
		return HITLEvent{}, err
	}
	if !ok {
		return HITLEvent{}, ErrHITLNotFound
	}
	return out, nil
}

// ExpireHITL flips a pending row to status=expired with responded_at=now.
// No-op (returns nil) when the row is not pending.
func (s *Store) ExpireHITL(ctx context.Context, hitlID string, now time.Time) error {
	const q = `
UPDATE hitl_events SET status = ?, responded_at = ?
WHERE hitl_id = ? AND status = ?
`
	_, err := s.db.ExecContext(ctx, q, HITLStatusExpired, now, hitlID, HITLStatusPending)
	if err != nil {
		return fmt.Errorf("expire hitl: %w", err)
	}
	return nil
}

// ListPendingHITLBySession returns every pending HITL for a session ordered
// by requested_at ascending (oldest first — matches the operator triage
// expectation).
func (s *Store) ListPendingHITLBySession(ctx context.Context, sessionID string) ([]HITLEvent, error) {
	const q = selectHITL + ` WHERE session_id = ? AND status = 'pending' ORDER BY requested_at ASC`
	return s.queryHITL(ctx, q, sessionID)
}

// ListPendingHITLByTurn returns every pending HITL scoped to a single turn.
func (s *Store) ListPendingHITLByTurn(ctx context.Context, turnID string) ([]HITLEvent, error) {
	const q = selectHITL + ` WHERE turn_id = ? AND status = 'pending' ORDER BY requested_at ASC`
	return s.queryHITL(ctx, q, turnID)
}

// ListHITLByTurn returns every HITL row associated with a turn.
func (s *Store) ListHITLByTurn(ctx context.Context, turnID string) ([]HITLEvent, error) {
	const q = selectHITL + ` WHERE turn_id = ? ORDER BY requested_at ASC`
	return s.queryHITL(ctx, q, turnID)
}

// ListExpiredCandidates returns every pending HITL whose requested_at is
// older than cutoff. Used by the auto-expiration loop.
func (s *Store) ListExpiredCandidates(ctx context.Context, cutoff time.Time, limit int) ([]HITLEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	const q = selectHITL + ` WHERE status = 'pending' AND requested_at < ? ORDER BY requested_at ASC LIMIT ?`
	return s.queryHITL(ctx, q, cutoff, limit)
}

// ListRecentHITL applies optional filters and returns the most recent rows
// first.
func (s *Store) ListRecentHITL(ctx context.Context, f HITLFilter, limit int) ([]HITLEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	q := selectHITL + ` WHERE 1=1`
	var args []any
	if f.SessionID != "" {
		q += ` AND session_id = ?`
		args = append(args, f.SessionID)
	}
	if f.Status != "" {
		q += ` AND status = ?`
		args = append(args, f.Status)
	}
	if f.Kind != "" {
		q += ` AND kind = ?`
		args = append(args, f.Kind)
	}
	q += ` ORDER BY requested_at DESC LIMIT ?`
	args = append(args, limit)
	return s.queryHITL(ctx, q, args...)
}

// CountPendingHITLEventsBySession returns pending HITL events grouped by
// session id. Mirrors CountPendingHITLBySession but reads from the typed
// hitl_events table instead of the raw spans table.
func (s *Store) CountPendingHITLEventsBySession(ctx context.Context, sessionIDs []string) (map[string]int64, error) {
	out := make(map[string]int64, len(sessionIDs))
	if len(sessionIDs) == 0 {
		return out, nil
	}
	placeholders := ""
	args := make([]any, 0, len(sessionIDs))
	for i, id := range sessionIDs {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args = append(args, id)
	}
	q := `SELECT session_id, COUNT(*) FROM hitl_events WHERE status = 'pending' AND session_id IN (` + placeholders + `) GROUP BY 1`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("count pending hitl events by session: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var n int64
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

func (s *Store) queryHITL(ctx context.Context, query string, args ...any) ([]HITLEvent, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query hitl: %w", err)
	}
	defer rows.Close()
	out := []HITLEvent{}
	for rows.Next() {
		ev, err := scanHITL(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

func scanHITL(r rowScanner) (HITLEvent, error) {
	var ev HITLEvent
	if err := r.Scan(
		&ev.ID,
		&ev.HitlID,
		&ev.SpanID,
		&ev.TraceID,
		&ev.SessionID,
		&ev.TurnID,
		&ev.Kind,
		&ev.Status,
		&ev.RequestedAt,
		&ev.RespondedAt,
		&ev.Question,
		&ev.SuggestionsJSON,
		&ev.ContextJSON,
		&ev.Decision,
		&ev.ReasonCategory,
		&ev.OperatorNote,
		&ev.ResumeMode,
		&ev.OperatorID,
	); err != nil {
		return HITLEvent{}, err
	}
	return ev, nil
}

func nullableSQLTime(t sql.NullTime) any {
	if !t.Valid {
		return nil
	}
	return t.Time
}

func nullableSQLString(v sql.NullString) any {
	if !v.Valid || v.String == "" {
		return nil
	}
	return v.String
}
