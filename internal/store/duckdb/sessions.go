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
func (s *Store) ListRecentSessions(ctx context.Context, limit int) ([]Session, error) {
	if limit <= 0 {
		limit = 100
	}
	const q = `SELECT session_id, source_app, started_at, ended_at, last_seen_at, turn_count, model, machine_id FROM sessions ORDER BY last_seen_at DESC LIMIT ?`
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

func nullString(v string) any {
	if v == "" {
		return nil
	}
	return v
}
