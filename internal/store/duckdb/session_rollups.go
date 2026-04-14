package duckdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// SessionRollup is the persisted row for one session's long-range digest.
// RollupJSON is kept opaque at the store layer so we don't import the
// summarizer package; callers unmarshal the blob on their own.
type SessionRollup struct {
	SessionID   string    `json:"session_id"`
	GeneratedAt time.Time `json:"generated_at"`
	Model       string    `json:"model"`
	FromTurnID  string    `json:"from_turn_id,omitempty"`
	ToTurnID    string    `json:"to_turn_id,omitempty"`
	TurnCount   int       `json:"turn_count"`
	RollupJSON  string    `json:"rollup_json"`
}

// UpsertSessionRollup writes or refreshes the rollup row for sessionID. The
// row is a full replacement — the summarizer worker is the only writer, so
// stale values are always safe to overwrite.
func (s *Store) UpsertSessionRollup(ctx context.Context, r SessionRollup) error {
	const q = `
INSERT INTO session_rollups (session_id, generated_at, model, from_turn_id, to_turn_id, turn_count, rollup_json)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (session_id) DO UPDATE SET
  generated_at  = excluded.generated_at,
  model         = excluded.model,
  from_turn_id  = excluded.from_turn_id,
  to_turn_id    = excluded.to_turn_id,
  turn_count    = excluded.turn_count,
  rollup_json   = excluded.rollup_json
`
	_, err := s.db.ExecContext(ctx, q,
		r.SessionID,
		r.GeneratedAt,
		r.Model,
		nullString(r.FromTurnID),
		nullString(r.ToTurnID),
		r.TurnCount,
		r.RollupJSON,
	)
	if err != nil {
		return fmt.Errorf("upsert session rollup: %w", err)
	}
	return nil
}

// GetSessionRollup returns the rollup row for sessionID, or (_, false, nil)
// when none has been written yet.
func (s *Store) GetSessionRollup(ctx context.Context, sessionID string) (SessionRollup, bool, error) {
	const q = `SELECT session_id, generated_at, model, from_turn_id, to_turn_id, turn_count, rollup_json FROM session_rollups WHERE session_id = ?`
	var (
		out        SessionRollup
		fromTurnID sql.NullString
		toTurnID   sql.NullString
	)
	err := s.db.QueryRowContext(ctx, q, sessionID).Scan(
		&out.SessionID,
		&out.GeneratedAt,
		&out.Model,
		&fromTurnID,
		&toTurnID,
		&out.TurnCount,
		&out.RollupJSON,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SessionRollup{}, false, nil
		}
		return SessionRollup{}, false, fmt.Errorf("get session rollup: %w", err)
	}
	if fromTurnID.Valid {
		out.FromTurnID = fromTurnID.String
	}
	if toTurnID.Valid {
		out.ToTurnID = toTurnID.String
	}
	return out, true, nil
}

// RollupCandidate is one row of the "sessions needing a rollup" query. The
// scheduler picks the top N and enqueues them.
type RollupCandidate struct {
	SessionID    string
	LastSeenAt   time.Time
	TurnCount    int
	LastRollupAt *time.Time
}

// ListRollupCandidates returns sessions with at least minTurns closed turns
// whose activity is newer than their last rollup (or have no rollup yet) and
// whose last rollup is older than minAgeSinceLast. Results are ordered by
// last_seen_at DESC and capped at limit.
//
// Sessions that were rolled up less than minAgeSinceLast ago are excluded so
// the hourly scheduler doesn't thrash on busy sessions.
func (s *Store) ListRollupCandidates(ctx context.Context, minTurns int, minAgeSinceLast time.Duration, limit int) ([]RollupCandidate, error) {
	if minTurns <= 0 {
		minTurns = 2
	}
	if limit <= 0 {
		limit = 5
	}
	cutoff := time.Now().Add(-minAgeSinceLast)
	const q = `
SELECT s.session_id,
       s.last_seen_at,
       s.turn_count,
       r.generated_at
FROM sessions s
LEFT JOIN session_rollups r ON r.session_id = s.session_id
WHERE s.turn_count >= ?
  AND (r.generated_at IS NULL OR (s.last_seen_at > r.generated_at AND r.generated_at < ?))
ORDER BY s.last_seen_at DESC
LIMIT ?
`
	rows, err := s.db.QueryContext(ctx, q, minTurns, cutoff, limit)
	if err != nil {
		return nil, fmt.Errorf("list rollup candidates: %w", err)
	}
	defer rows.Close()
	var out []RollupCandidate
	for rows.Next() {
		var (
			c            RollupCandidate
			lastRollupAt sql.NullTime
		)
		if err := rows.Scan(&c.SessionID, &c.LastSeenAt, &c.TurnCount, &lastRollupAt); err != nil {
			return nil, fmt.Errorf("scan rollup candidate: %w", err)
		}
		if lastRollupAt.Valid {
			v := lastRollupAt.Time
			c.LastRollupAt = &v
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
