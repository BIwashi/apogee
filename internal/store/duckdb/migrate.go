package duckdb

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
)

//go:embed schema.sql
var schemaSQL string

// migrate applies the embedded schema. DuckDB does not support multi-statement
// Exec for every statement type, so we split on semicolons and execute each
// one individually.
func (s *Store) migrate(ctx context.Context) error {
	for _, stmt := range splitStatements(schemaSQL) {
		if stmt == "" {
			continue
		}
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("apply schema statement %q: %w", firstLine(stmt), err)
		}
	}
	return s.applyColumnAdditions(ctx)
}

// applyColumnAdditions is a minimal post-schema migration step that adds
// columns the attention engine depends on to existing turns tables. DuckDB
// does not reliably support ADD COLUMN IF NOT EXISTS, so we guard each ALTER
// with a catalog lookup against duckdb_columns().
func (s *Store) applyColumnAdditions(ctx context.Context) error {
	additions := []struct {
		table  string
		column string
		sql    string
	}{
		{"turns", "attention_tone", `ALTER TABLE turns ADD COLUMN attention_tone VARCHAR`},
		{"turns", "phase", `ALTER TABLE turns ADD COLUMN phase VARCHAR`},
		{"turns", "phase_confidence", `ALTER TABLE turns ADD COLUMN phase_confidence DOUBLE`},
		{"turns", "phase_since", `ALTER TABLE turns ADD COLUMN phase_since TIMESTAMP`},
		{"turns", "attention_signals_json", `ALTER TABLE turns ADD COLUMN attention_signals_json VARCHAR`},
		{"turns", "recap_json", `ALTER TABLE turns ADD COLUMN recap_json VARCHAR`},
		{"turns", "recap_generated_at", `ALTER TABLE turns ADD COLUMN recap_generated_at TIMESTAMP`},
		{"turns", "recap_model", `ALTER TABLE turns ADD COLUMN recap_model VARCHAR`},
		{"turns", "topic_id", `ALTER TABLE turns ADD COLUMN topic_id VARCHAR`},
		{"sessions", "attention_state", `ALTER TABLE sessions ADD COLUMN attention_state VARCHAR`},
		{"sessions", "attention_reason", `ALTER TABLE sessions ADD COLUMN attention_reason VARCHAR`},
		{"sessions", "attention_score", `ALTER TABLE sessions ADD COLUMN attention_score DOUBLE`},
		{"sessions", "current_turn_id", `ALTER TABLE sessions ADD COLUMN current_turn_id VARCHAR`},
		{"sessions", "current_phase", `ALTER TABLE sessions ADD COLUMN current_phase VARCHAR`},
		{"sessions", "live_state", `ALTER TABLE sessions ADD COLUMN live_state VARCHAR`},
		{"sessions", "live_status_text", `ALTER TABLE sessions ADD COLUMN live_status_text VARCHAR`},
		{"sessions", "live_status_at", `ALTER TABLE sessions ADD COLUMN live_status_at TIMESTAMP`},
		{"sessions", "live_status_model", `ALTER TABLE sessions ADD COLUMN live_status_model VARCHAR`},
	}
	for _, a := range additions {
		var count int
		err := s.db.QueryRowContext(
			ctx,
			`SELECT COUNT(*) FROM duckdb_columns() WHERE table_name = ? AND column_name = ?`,
			a.table, a.column,
		).Scan(&count)
		if err != nil {
			return fmt.Errorf("probe column %s.%s: %w", a.table, a.column, err)
		}
		if count > 0 {
			continue
		}
		if _, err := s.db.ExecContext(ctx, a.sql); err != nil {
			return fmt.Errorf("add column %s.%s: %w", a.table, a.column, err)
		}
	}

	// Post-column indexes — these reference columns that only exist after
	// the additions above, so they cannot live in schema.sql (which runs
	// before applyColumnAdditions on existing databases).
	postIndexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_sessions_attention ON sessions(attention_state)`,
	}
	for _, idx := range postIndexes {
		if _, err := s.db.ExecContext(ctx, idx); err != nil {
			return fmt.Errorf("create post-migration index: %w", err)
		}
	}
	return nil
}

func splitStatements(src string) []string {
	parts := strings.Split(src, ";")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(stripComments(p))
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

// stripComments removes leading SQL line comments so they do not become empty
// statements after splitting.
func stripComments(s string) string {
	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "--") {
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
