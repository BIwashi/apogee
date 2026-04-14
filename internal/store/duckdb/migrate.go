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
