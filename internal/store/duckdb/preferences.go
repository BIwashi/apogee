package duckdb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Preference is one row from the user_preferences table. Value is the raw
// JSON-encoded payload as it lives on disk; callers unmarshal into the
// concrete shape they care about (string, number, struct, ...). Storing JSON
// keeps the table generic so new keys can land without a schema migration.
type Preference struct {
	Key       string          `json:"key"`
	Value     json.RawMessage `json:"value"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// UpsertPreference writes value at key. value is JSON-encoded with
// encoding/json before being stored. Passing a json.RawMessage avoids the
// extra round-trip when the caller already holds the encoded bytes.
func (s *Store) UpsertPreference(ctx context.Context, key string, value any) error {
	if key == "" {
		return errors.New("preferences: empty key")
	}
	var blob []byte
	switch v := value.(type) {
	case json.RawMessage:
		if len(v) == 0 {
			blob = []byte("null")
		} else {
			blob = []byte(v)
		}
	case []byte:
		// Caller passed pre-encoded JSON; trust it but fall back to
		// re-marshal if it does not parse.
		if json.Valid(v) {
			blob = v
		} else {
			b, err := json.Marshal(string(v))
			if err != nil {
				return fmt.Errorf("preferences: marshal raw bytes for key %q: %w", key, err)
			}
			blob = b
		}
	default:
		b, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("preferences: marshal value for key %q: %w", key, err)
		}
		blob = b
	}
	now := time.Now().UTC()
	const q = `
INSERT INTO user_preferences (key, value_json, updated_at)
VALUES (?, ?, ?)
ON CONFLICT (key) DO UPDATE SET
  value_json = excluded.value_json,
  updated_at = excluded.updated_at
`
	if _, err := s.db.ExecContext(ctx, q, key, string(blob), now); err != nil {
		return fmt.Errorf("preferences: upsert %q: %w", key, err)
	}
	return nil
}

// GetPreference returns the row for key. The bool return is false when the
// row does not exist; the error return is non-nil only on real database
// failures so callers can reliably tell "missing" from "broken".
func (s *Store) GetPreference(ctx context.Context, key string) (Preference, bool, error) {
	if key == "" {
		return Preference{}, false, errors.New("preferences: empty key")
	}
	const q = `SELECT key, value_json, updated_at FROM user_preferences WHERE key = ?`
	var (
		row     Preference
		valStr  string
		updated time.Time
	)
	err := s.db.QueryRowContext(ctx, q, key).Scan(&row.Key, &valStr, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Preference{}, false, nil
	}
	if err != nil {
		return Preference{}, false, fmt.Errorf("preferences: get %q: %w", key, err)
	}
	row.Value = json.RawMessage(valStr)
	row.UpdatedAt = updated.UTC()
	return row, true, nil
}

// ListPreferences returns every preference row in key-ascending order.
func (s *Store) ListPreferences(ctx context.Context) ([]Preference, error) {
	const q = `SELECT key, value_json, updated_at FROM user_preferences ORDER BY key ASC`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("preferences: list: %w", err)
	}
	defer rows.Close()
	var out []Preference
	for rows.Next() {
		var (
			p       Preference
			valStr  string
			updated time.Time
		)
		if err := rows.Scan(&p.Key, &valStr, &updated); err != nil {
			return nil, fmt.Errorf("preferences: scan: %w", err)
		}
		p.Value = json.RawMessage(valStr)
		p.UpdatedAt = updated.UTC()
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("preferences: rows: %w", err)
	}
	return out, nil
}

// DeletePreference removes the row at key. A missing row is not an error.
func (s *Store) DeletePreference(ctx context.Context, key string) error {
	if key == "" {
		return errors.New("preferences: empty key")
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM user_preferences WHERE key = ?`, key); err != nil {
		return fmt.Errorf("preferences: delete %q: %w", key, err)
	}
	return nil
}
