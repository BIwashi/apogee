package duckdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ModelAvailability is one row of the model_availability cache table.
// Values are written by summarizer.Probe via UpsertModelAvailability and
// consumed by the /v1/models handler and the summarizer workers.
type ModelAvailability struct {
	Alias     string    `json:"alias"`
	Available bool      `json:"available"`
	CheckedAt time.Time `json:"checked_at"`
	LastError string    `json:"last_error"`
}

// UpsertModelAvailability writes or replaces the row for alias.
// A row with available=true and an empty last_error is considered a
// successful probe. available=false + last_error carries the truncated
// failure reason so operators can inspect the cache.
func (s *Store) UpsertModelAvailability(ctx context.Context, alias string, available bool, lastError string) error {
	if alias == "" {
		return errors.New("model_availability: empty alias")
	}
	const q = `
INSERT INTO model_availability (alias, available, checked_at, last_error)
VALUES (?, ?, ?, ?)
ON CONFLICT (alias) DO UPDATE SET
  available  = excluded.available,
  checked_at = excluded.checked_at,
  last_error = excluded.last_error
`
	now := time.Now().UTC()
	if _, err := s.db.ExecContext(ctx, q, alias, available, now, lastError); err != nil {
		return fmt.Errorf("model_availability: upsert %q: %w", alias, err)
	}
	return nil
}

// GetModelAvailability returns every row in the cache keyed by alias.
// A missing table or empty cache yield an empty (non-nil) map.
func (s *Store) GetModelAvailability(ctx context.Context) (map[string]ModelAvailability, error) {
	const q = `SELECT alias, available, checked_at, last_error FROM model_availability`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("model_availability: list: %w", err)
	}
	defer rows.Close()
	out := map[string]ModelAvailability{}
	for rows.Next() {
		var (
			row     ModelAvailability
			lastErr sql.NullString
			checked time.Time
		)
		if err := rows.Scan(&row.Alias, &row.Available, &checked, &lastErr); err != nil {
			return nil, fmt.Errorf("model_availability: scan: %w", err)
		}
		row.CheckedAt = checked.UTC()
		if lastErr.Valid {
			row.LastError = lastErr.String
		}
		out[row.Alias] = row
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("model_availability: rows: %w", err)
	}
	return out, nil
}

// PruneStaleAvailability deletes every row whose checked_at is older
// than the supplied cutoff. Returns the number of rows removed.
func (s *Store) PruneStaleAvailability(ctx context.Context, olderThan time.Time) (int64, error) {
	const q = `DELETE FROM model_availability WHERE checked_at < ?`
	res, err := s.db.ExecContext(ctx, q, olderThan.UTC())
	if err != nil {
		return 0, fmt.Errorf("model_availability: prune: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}
