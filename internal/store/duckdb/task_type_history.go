package duckdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// PatternStats is the dashboard-facing projection of one task_type_history
// row. Callers convert it to the attention.PatternStats type when needed.
type PatternStats struct {
	Pattern      string    `json:"pattern"`
	TurnCount    int       `json:"turn_count"`
	SuccessCount int       `json:"success_count"`
	FailureCount int       `json:"failure_count"`
	LastUpdated  time.Time `json:"last_updated"`
}

// UpsertPatternOutcome records the outcome of a closed turn against its
// canonical tool pattern. Success increments success_count, failure
// increments failure_count, and turn_count bumps in either case.
func (s *Store) UpsertPatternOutcome(ctx context.Context, pattern string, success bool, now time.Time) error {
	if pattern == "" {
		return nil
	}
	// DuckDB ON CONFLICT requires values for every new row column, so we
	// pre-compute the initial success/failure counts.
	successDelta, failureDelta := 0, 0
	if success {
		successDelta = 1
	} else {
		failureDelta = 1
	}
	const q = `
INSERT INTO task_type_history (pattern, turn_count, success_count, failure_count, last_updated)
VALUES (?, 1, ?, ?, ?)
ON CONFLICT (pattern) DO UPDATE SET
  turn_count    = task_type_history.turn_count + 1,
  success_count = task_type_history.success_count + excluded.success_count,
  failure_count = task_type_history.failure_count + excluded.failure_count,
  last_updated  = GREATEST(task_type_history.last_updated, excluded.last_updated)
`
	if _, err := s.db.ExecContext(ctx, q, pattern, successDelta, failureDelta, now); err != nil {
		return fmt.Errorf("upsert pattern outcome: %w", err)
	}
	return nil
}

// GetPatternStats fetches a single pattern row. Returns a zero-value
// PatternStats (with the given pattern) and no error when the pattern has
// not been seen yet.
func (s *Store) GetPatternStats(ctx context.Context, pattern string) (PatternStats, error) {
	const q = `
SELECT pattern, turn_count, success_count, failure_count, last_updated
FROM task_type_history
WHERE pattern = ?
`
	row := s.db.QueryRowContext(ctx, q, pattern)
	var ps PatternStats
	if err := row.Scan(&ps.Pattern, &ps.TurnCount, &ps.SuccessCount, &ps.FailureCount, &ps.LastUpdated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return PatternStats{Pattern: pattern}, nil
		}
		return PatternStats{}, fmt.Errorf("get pattern stats: %w", err)
	}
	return ps, nil
}

// ListTopPatterns returns the N most recently updated patterns. Useful for
// the future "hot patterns" panel.
func (s *Store) ListTopPatterns(ctx context.Context, limit int) ([]PatternStats, error) {
	if limit <= 0 {
		limit = 20
	}
	const q = `
SELECT pattern, turn_count, success_count, failure_count, last_updated
FROM task_type_history
ORDER BY last_updated DESC
LIMIT ?
`
	rows, err := s.db.QueryContext(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("list top patterns: %w", err)
	}
	defer rows.Close()
	var out []PatternStats
	for rows.Next() {
		var ps PatternStats
		if err := rows.Scan(&ps.Pattern, &ps.TurnCount, &ps.SuccessCount, &ps.FailureCount, &ps.LastUpdated); err != nil {
			return nil, err
		}
		out = append(out, ps)
	}
	return out, rows.Err()
}
