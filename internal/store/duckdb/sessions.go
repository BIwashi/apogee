package duckdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Session is a row in the sessions table.
type Session struct {
	SessionID  string     `json:"session_id"`
	SourceApp  string     `json:"source_app"`
	StartedAt  time.Time  `json:"started_at"`
	EndedAt    *time.Time `json:"ended_at,omitempty"`
	LastSeenAt time.Time  `json:"last_seen_at"`
	TurnCount  int        `json:"turn_count"`
	Model      string     `json:"model,omitempty"`
	MachineID  string     `json:"machine_id,omitempty"`
}

// UpsertSession inserts or refreshes a sessions row. started_at is preserved
// across upserts; last_seen_at and source_app/model are kept up to date.
func (s *Store) UpsertSession(ctx context.Context, sess Session) error {
	const q = `
INSERT INTO sessions (session_id, source_app, started_at, last_seen_at, turn_count, model, machine_id)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (session_id) DO UPDATE SET
  source_app   = excluded.source_app,
  last_seen_at = GREATEST(sessions.last_seen_at, excluded.last_seen_at),
  model        = COALESCE(excluded.model, sessions.model),
  machine_id   = COALESCE(excluded.machine_id, sessions.machine_id)
`
	_, err := s.db.ExecContext(ctx, q,
		sess.SessionID,
		sess.SourceApp,
		sess.StartedAt,
		sess.LastSeenAt,
		sess.TurnCount,
		nullString(sess.Model),
		nullString(sess.MachineID),
	)
	if err != nil {
		return fmt.Errorf("upsert session: %w", err)
	}
	return nil
}

// MarkSessionEnded sets ended_at on a session row.
func (s *Store) MarkSessionEnded(ctx context.Context, sessionID string, endedAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET ended_at = ?, last_seen_at = GREATEST(last_seen_at, ?) WHERE session_id = ?`, endedAt, endedAt, sessionID)
	if err != nil {
		return fmt.Errorf("mark session ended: %w", err)
	}
	return nil
}

// IncrementSessionTurnCount bumps the rolling turn count for a session.
func (s *Store) IncrementSessionTurnCount(ctx context.Context, sessionID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET turn_count = turn_count + 1 WHERE session_id = ?`, sessionID)
	if err != nil {
		return fmt.Errorf("increment turn count: %w", err)
	}
	return nil
}

// GetSession fetches one session row by id.
func (s *Store) GetSession(ctx context.Context, sessionID string) (*Session, error) {
	const q = `SELECT session_id, source_app, started_at, ended_at, last_seen_at, turn_count, model, machine_id FROM sessions WHERE session_id = ?`
	row := s.db.QueryRowContext(ctx, q, sessionID)
	var (
		out       Session
		endedAt   sql.NullTime
		model     sql.NullString
		machineID sql.NullString
	)
	if err := row.Scan(&out.SessionID, &out.SourceApp, &out.StartedAt, &endedAt, &out.LastSeenAt, &out.TurnCount, &model, &machineID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get session: %w", err)
	}
	if endedAt.Valid {
		t := endedAt.Time
		out.EndedAt = &t
	}
	if model.Valid {
		out.Model = model.String
	}
	if machineID.Valid {
		out.MachineID = machineID.String
	}
	return &out, nil
}

// ListRecentSessions returns up to limit sessions ordered by last_seen_at DESC.
//
// Sessions whose source_app starts with "." are filtered out because
// those are the synthetic rows created by the summarizer feedback
// loop (the `claude` subprocess's cwd basename ends up as the
// source_app, and the daemon runs from ~/.apogee). Real source_app
// values can never legitimately start with a dot — the apogee hook's
// derivation rules (env var > git toplevel > cwd basename) would
// never pick a hidden directory — so this filter is a safe global
// cleanup that also hides any legacy rows left over from before the
// APOGEE_HOOK_SKIP guard was added.
func (s *Store) ListRecentSessions(ctx context.Context, limit int) ([]Session, error) {
	if limit <= 0 {
		limit = 100
	}
	const q = `SELECT session_id, source_app, started_at, ended_at, last_seen_at, turn_count, model, machine_id FROM sessions WHERE source_app NOT LIKE '.%' ORDER BY last_seen_at DESC LIMIT ?`
	rows, err := s.db.QueryContext(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		var (
			sess      Session
			endedAt   sql.NullTime
			model     sql.NullString
			machineID sql.NullString
		)
		if err := rows.Scan(&sess.SessionID, &sess.SourceApp, &sess.StartedAt, &endedAt, &sess.LastSeenAt, &sess.TurnCount, &model, &machineID); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		if endedAt.Valid {
			t := endedAt.Time
			sess.EndedAt = &t
		}
		if model.Valid {
			sess.Model = model.String
		}
		if machineID.Valid {
			sess.MachineID = machineID.String
		}
		out = append(out, sess)
	}
	return out, rows.Err()
}

// SessionSearchHit is one row in the /v1/sessions/search response. It is
// enriched with the latest turn's headline / prompt snippet so the command
// palette can render a single-line label per session.
type SessionSearchHit struct {
	SessionID           string    `json:"session_id"`
	SourceApp           string    `json:"source_app"`
	LastSeenAt          time.Time `json:"last_seen_at"`
	TurnCount           int       `json:"turn_count"`
	LatestHeadline      string    `json:"latest_headline,omitempty"`
	LatestPromptSnippet string    `json:"latest_prompt_snippet,omitempty"`
	AttentionState      string    `json:"attention_state,omitempty"`
}

// SearchSessions returns up to limit sessions matching q. Empty q returns
// the most-recent sessions unfiltered.
func (s *Store) SearchSessions(ctx context.Context, q string, limit int) ([]SessionSearchHit, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	// The inner latest_turn CTE picks the newest turn per session so the
	// headline + prompt + attention state come from the most-recent row.
	query := `
WITH latest_turn AS (
  SELECT t.session_id,
         t.headline,
         t.prompt_text,
         t.attention_state,
         t.started_at,
         ROW_NUMBER() OVER (PARTITION BY t.session_id ORDER BY t.started_at DESC) AS rn
  FROM turns t
)
SELECT s.session_id,
       s.source_app,
       s.last_seen_at,
       s.turn_count,
       COALESCE(lt.headline, ''),
       COALESCE(lt.prompt_text, ''),
       COALESCE(lt.attention_state, '')
FROM sessions s
LEFT JOIN latest_turn lt ON lt.session_id = s.session_id AND lt.rn = 1
WHERE s.source_app NOT LIKE '.%'
`
	args := []any{}
	if q != "" {
		query += `
  AND (
        s.session_id LIKE ?
     OR s.source_app LIKE ?
     OR COALESCE(lt.prompt_text, '') LIKE ?
     OR COALESCE(lt.headline, '') LIKE ?
  )
`
		like := "%" + q + "%"
		prefix := q + "%"
		args = append(args, prefix, like, like, like)
	}
	query += ` ORDER BY s.last_seen_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("search sessions: %w", err)
	}
	defer rows.Close()
	out := []SessionSearchHit{}
	for rows.Next() {
		var h SessionSearchHit
		var headline, prompt, attn string
		if err := rows.Scan(&h.SessionID, &h.SourceApp, &h.LastSeenAt, &h.TurnCount, &headline, &prompt, &attn); err != nil {
			return nil, fmt.Errorf("scan search hit: %w", err)
		}
		h.LatestHeadline = headline
		if prompt != "" {
			snippet := prompt
			if len(snippet) > 120 {
				snippet = snippet[:120]
			}
			h.LatestPromptSnippet = snippet
		}
		h.AttentionState = attn
		out = append(out, h)
	}
	return out, rows.Err()
}

// SessionSummary is the denormalized header surfaced by
// GET /v1/sessions/:id/summary.
type SessionSummary struct {
	SessionID      string     `json:"session_id"`
	SourceApp      string     `json:"source_app"`
	StartedAt      time.Time  `json:"started_at"`
	LastSeenAt     time.Time  `json:"last_seen_at"`
	EndedAt        *time.Time `json:"ended_at,omitempty"`
	Model          string     `json:"model,omitempty"`
	MachineID      string     `json:"machine_id,omitempty"`
	TurnCount      int        `json:"turn_count"`
	RunningCount   int        `json:"running_count"`
	CompletedCount int        `json:"completed_count"`
	ErroredCount   int        `json:"errored_count"`
	LatestHeadline string     `json:"latest_headline,omitempty"`
	LatestTurnID   string     `json:"latest_turn_id,omitempty"`
	AttentionState string     `json:"attention_state,omitempty"`
}

// attentionPriority maps the four engine states onto a compare-friendly
// integer. Lower is more urgent.
func attentionPriority(state string) int {
	switch state {
	case "intervene_now":
		return 0
	case "watch":
		return 1
	case "watchlist":
		return 2
	case "healthy":
		return 3
	}
	return 4
}

// GetSessionSummary rolls up the turns belonging to sessionID into a single
// header row. Returns (nil, nil) when the session does not exist.
func (s *Store) GetSessionSummary(ctx context.Context, sessionID string) (*SessionSummary, error) {
	sess, err := s.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if sess == nil {
		return nil, nil
	}
	out := &SessionSummary{
		SessionID:  sess.SessionID,
		SourceApp:  sess.SourceApp,
		StartedAt:  sess.StartedAt,
		LastSeenAt: sess.LastSeenAt,
		EndedAt:    sess.EndedAt,
		Model:      sess.Model,
		MachineID:  sess.MachineID,
		TurnCount:  sess.TurnCount,
	}

	// Status breakdown.
	rows, err := s.db.QueryContext(ctx, `SELECT COALESCE(status, '') AS st, COUNT(*) FROM turns WHERE session_id = ? GROUP BY 1`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("session summary counts: %w", err)
	}
	for rows.Next() {
		var st string
		var c int
		if err := rows.Scan(&st, &c); err != nil {
			rows.Close()
			return nil, err
		}
		switch st {
		case "running":
			out.RunningCount = c
		case "completed":
			out.CompletedCount = c
		case "errored":
			out.ErroredCount = c
		}
	}
	rows.Close()

	// Highest-priority attention state across all turns.
	attnRows, err := s.db.QueryContext(ctx, `SELECT DISTINCT attention_state FROM turns WHERE session_id = ? AND attention_state IS NOT NULL`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("session summary attention: %w", err)
	}
	bestPri := 99
	for attnRows.Next() {
		var st sql.NullString
		if err := attnRows.Scan(&st); err != nil {
			attnRows.Close()
			return nil, err
		}
		if !st.Valid || st.String == "" {
			continue
		}
		p := attentionPriority(st.String)
		if p < bestPri {
			bestPri = p
			out.AttentionState = st.String
		}
	}
	attnRows.Close()

	// Latest turn headline and id.
	var headline, latestTurnID sql.NullString
	err = s.db.QueryRowContext(ctx, `
SELECT turn_id, COALESCE(headline, '')
FROM turns
WHERE session_id = ?
ORDER BY started_at DESC
LIMIT 1
`, sessionID).Scan(&latestTurnID, &headline)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("session summary latest turn: %w", err)
	}
	if latestTurnID.Valid {
		out.LatestTurnID = latestTurnID.String
	}
	if headline.Valid {
		out.LatestHeadline = headline.String
	}
	return out, nil
}

// RecentSessionsWithActivity returns up to n session IDs ordered by
// last_seen_at DESC. Used by the metrics collector to bound its per-session
// label write cost.
func (s *Store) RecentSessionsWithActivity(ctx context.Context, n int) ([]Session, error) {
	if n <= 0 {
		n = 20
	}
	return s.ListRecentSessions(ctx, n)
}

func nullString(v string) any {
	if v == "" {
		return nil
	}
	return v
}
